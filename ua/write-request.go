// Copyright 2018-2019 opcua authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package ua

// WriteRequest is used to write values to one or more Attributes of one or more Nodes. For
// constructed Attribute values whose elements are indexed, such as an array, this Service allows
// Clients to write the entire set of indexed values as a composite, to write individual elements or to
// write ranges of elements of the composite.
//
// Specification: Part 4, 5.10.4
type WriteRequest struct {
	RequestHeader *RequestHeader
	NodesToWrite  []*WriteValue
}

// NewWriteRequest creates a new WriteRequest.
func NewWriteRequest(reqHeader *RequestHeader, nodes ...*WriteValue) *WriteRequest {
	return &WriteRequest{
		RequestHeader: reqHeader,
		NodesToWrite:  nodes,
	}
}
