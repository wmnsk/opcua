package uasc

import (
	"crypto/rand"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/gopcua/opcua/uacp"
)

const (
	secureChannelCreated int32 = iota
	secureChannelOpen
	secureChannelClosed
)

type Response struct {
	V   interface{}
	Err error
}

type SecureChannel struct {
	EndpointURL string

	// c is the uacp connection.
	c *uacp.Conn

	// cfg is the configuration for the secure channel.
	cfg *Config

	// reqhdr is the header for the next request.
	reqhdr *ua.RequestHeader

	// quit signals the termination of the recv loop.
	quit chan struct{}

	// state is the state of the secure channel.
	// Must be accessed with atomic.LoadInt32/StoreInt32
	state int32

	// mu guards handler which contains the response channels
	// for the outstanding requests. The key is the request
	// handle which is part of the Request and Response headers.
	mu      sync.Mutex
	handler map[uint32]chan Response
}

func init() {
	mrand.Seed(time.Now().UnixNano())
}

func NewSecureChannel(c *uacp.Conn, cfg *Config) *SecureChannel {
	if cfg == nil {
		cfg = NewClientConfigSecurityNone(uint32(mrand.Int31()), 3600000)
	}

	// always reset the secure channel id
	cfg.SecureChannelID = 0

	reqhdr := &ua.RequestHeader{
		AuthenticationToken: ua.NewTwoByteNodeID(0),
		Timestamp:           time.Now(),
		TimeoutHint:         0xffff,
		AdditionalHeader:    ua.NewNullAdditionalHeader(),
	}

	return &SecureChannel{
		c:       c,
		cfg:     cfg,
		reqhdr:  reqhdr,
		state:   secureChannelCreated,
		quit:    make(chan struct{}),
		handler: make(map[uint32]chan Response),
	}
}

func (s *SecureChannel) Open() error {
	go s.recv()
	return s.openSecureChannel()
}

func (s *SecureChannel) Close() error {
	if err := s.closeSecureChannel(); err != nil {
		log.Print("failed to send close secure channel request")
	}
	close(s.quit)
	return s.c.Close()
}

func (s *SecureChannel) LocalEndpoint() string {
	return s.EndpointURL
}

func (s *SecureChannel) openSecureChannel() error {
	// todo(fs): do we need to set the nonce if the security policy is None?
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}

	req := &ua.OpenSecureChannelRequest{
		ClientProtocolVersion:    0,
		SecurityTokenRequestType: ua.ReqTypeIssue,
		MessageSecurityMode:      s.cfg.SecurityMode,
		ClientNonce:              nonce,
		RequestedLifetime:        s.cfg.Lifetime,
	}

	return s.Send(req, func(v interface{}) error {
		resp, ok := v.(*ua.OpenSecureChannelResponse)
		if !ok {
			return fmt.Errorf("got %T, want OpenSecureChannelResponse", req)
		}
		s.cfg.SecurityTokenID = resp.SecurityToken.TokenID
		atomic.StoreInt32(&s.state, secureChannelOpen)
		return nil
	})
}

// closeSecureChannel sends CloseSecureChannelRequest on top of UASC to SecureChannel.
func (s *SecureChannel) closeSecureChannel() error {
	req := &ua.CloseSecureChannelRequest{
		SecureChannelID: s.cfg.SecureChannelID,
	}

	defer atomic.StoreInt32(&s.state, secureChannelClosed)
	return s.Send(req, nil)
}

// Send sends the service request and calls h with the response.
func (s *SecureChannel) Send(svc interface{}, h func(interface{}) error) error {
	ch, err := s.SendAsync(svc)
	if err != nil {
		return err
	}

	if h == nil {
		return nil
	}

	// todo(fs): handle timeout
	resp := <-ch
	if resp.Err != nil {
		return resp.Err
	}
	return h(resp.V)
}

// SendAsync sends the service request and returns a channel which will receive the
// response when it arrives.
func (s *SecureChannel) SendAsync(svc interface{}) (resp chan Response, err error) {
	typeID := ua.TypeID(svc)
	if typeID == 0 {
		return nil, fmt.Errorf("unknown service %T. Did you call register?", svc)
	}

	// the request header is always the first field
	val := reflect.ValueOf(svc)
	val.Elem().Field(0).Set(reflect.ValueOf(s.reqhdr))

	// update counters and reset them on error
	s.cfg.SequenceNumber++
	s.reqhdr.RequestHandle++
	s.reqhdr.Timestamp = time.Now()
	defer func() {
		if err != nil {
			s.cfg.SequenceNumber--
			s.reqhdr.RequestHandle--
		}
	}()

	// encode the message
	m := NewMessage(svc, typeID, s.cfg)
	b, err := m.Encode()
	if err != nil {
		return nil, err
	}
	reqid := m.SequenceHeader.RequestID

	// send the message
	if _, err := s.c.Write(b); err != nil {
		return nil, err
	}
	log.Printf("conn %d/%d: send %T with %d bytes", s.c.ID(), reqid, svc, len(b))

	// register the handler
	resp = make(chan Response)
	s.mu.Lock()
	s.handler[reqid] = resp
	s.mu.Unlock()
	return resp, nil
}

func (s *SecureChannel) readchunk() (*MessageChunk, error) {
	// read and decode the header to get the message size
	const hdrlen = 12
	b := make([]byte, s.c.ReceiveBufSize())
	_, err := io.ReadFull(s.c, b[:hdrlen])
	if err == io.EOF {
		return nil, err
	}
	if atomic.LoadInt32(&s.state) == secureChannelClosed {
		return nil, io.EOF
	}
	if err != nil {
		return nil, fmt.Errorf("sechan: read header failed: %s %#v", err, err)
	}

	h := new(Header)
	if _, err := h.Decode(b[:hdrlen]); err != nil {
		return nil, fmt.Errorf("sechan: decode header failed: %s", err)
	}
	b = b[:h.MessageSize]

	// drop if the channel id does not match
	if s.cfg.SecureChannelID > 0 && s.cfg.SecureChannelID != h.SecureChannelID {
		return nil, fmt.Errorf("sechan: secure channel id mismatch: got 0x%04x, want 0x%04x", h.SecureChannelID, s.cfg.SecureChannelID)
	}

	// read the rest of the message
	if _, err := io.ReadFull(s.c, b[hdrlen:]); err != nil {
		return nil, fmt.Errorf("sechan: read message failed")
	}

	// decode the other headers
	m := new(MessageChunk)
	if _, err := m.Decode(b); err != nil {
		return nil, fmt.Errorf("sechan: decode message failed: %s", err)
	}

	// todo(fs): handle ERR messages
	// todo(fs): handle crypto

	if s.cfg.SecureChannelID == 0 {
		s.cfg.SecureChannelID = h.SecureChannelID
		log.Printf("conn %d/%d: set secure channel id to %d", s.c.ID(), m.SequenceHeader.RequestID, s.cfg.SecureChannelID)
	}

	return m, nil
}

// recv receives message chunks from the secure channel, decodes and forwards
// them to the registered callback channel, if there is one. Otherwise,
// the message is dropped.
func (s *SecureChannel) recv() {
	// chunks maps request id to message chunks
	chunks := map[uint32][]*MessageChunk{}

	for {
		select {
		case <-s.quit:
			return

		default:
			chunk, err := s.readchunk()
			if err == io.EOF {
				return
			}

			hdr := chunk.Header
			reqid := chunk.SequenceHeader.RequestID
			log.Printf("conn %d/%d: recv %s%c with %d bytes", s.c.ID(), reqid, hdr.MessageType, hdr.ChunkType, hdr.MessageSize)

			if hdr.ChunkType != 'F' {
				chunks[reqid] = append(chunks[reqid], chunk)
				if n := len(chunks[reqid]); uint32(n) > s.c.MaxChunkCount() {
					// todo(fs): send error
					delete(chunks, reqid)
					s.notifyCaller(reqid, nil, fmt.Errorf("too many chunks: %d > %d", n, s.c.MaxChunkCount()))
				}
				continue
			}

			// merge chunks
			all := append(chunks[reqid], chunk)
			delete(chunks, reqid)
			b, err := mergeChunks(all)
			if err != nil {
				// todo(fs): send error
				s.notifyCaller(reqid, nil, fmt.Errorf("chunk merge error: %v", err))
				continue
			}

			if uint32(len(b)) > s.c.MaxMessageSize() {
				// todo(fs): send error
				s.notifyCaller(reqid, nil, fmt.Errorf("message too large: %d > %d", uint32(len(b)), s.c.MaxMessageSize()))
				continue
			}
			// fmt.Println(utils.Wireshark(0, b))

			// since we are not decoding the ResponseHeader separately
			// we need to drop every message that has an error since we
			// cannot get to the RequestHandle in the ResponseHeader.
			// To fix this we must a) decode the ResponseHeader separately
			// and subsequently remove it and the TypeID from all service
			// structs and tests. We also need to add a deadline to all
			// handlers and check them periodically to time them out.
			_, svc, err := ua.DecodeService(b)
			if err != nil {
				s.notifyCaller(reqid, nil, err)
				continue
			}
			s.notifyCaller(reqid, svc, err)
		}
	}
}

func (s *SecureChannel) notifyCaller(reqid uint32, svc interface{}, err error) {
	if err != nil {
		log.Printf("conn %d/%d: %v", s.c.ID(), reqid, err)
	} else {
		log.Printf("conn %d/%d: recv %T", s.c.ID(), reqid, svc)
	}

	// check if we have a pending request handler for this response.
	s.mu.Lock()
	ch := s.handler[reqid]
	delete(s.handler, reqid)
	s.mu.Unlock()

	// no handler -> next response
	if ch == nil {
		log.Printf("conn %d/%d: no handler for %T", s.c.ID(), reqid, svc)
		return
	}

	// send response to caller
	go func() {
		ch <- Response{svc, err}
		close(ch)
	}()
}

func mergeChunks(chunks []*MessageChunk) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	if len(chunks) == 1 {
		return chunks[0].Data, nil
	}

	// todo(fs): check if this is correct and necessary
	// sort.Sort(bySequence(chunks))

	var b []byte
	var seqnr uint32
	for _, c := range chunks {
		if c.SequenceHeader.SequenceNumber == seqnr {
			continue // duplicate chunk
		}
		seqnr = c.SequenceHeader.SequenceNumber
		b = append(b, c.Data...)
	}
	return b, nil
}

// todo(fs): we only need this if we need to sort chunks. Need to check the spec
// type bySequence []*MessageChunk

// func (a bySequence) Len() int      { return len(a) }
// func (a bySequence) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
// func (a bySequence) Less(i, j int) bool {
// 	return a[i].SequenceHeader.SequenceNumber < a[j].SequenceHeader.SequenceNumber
// }
