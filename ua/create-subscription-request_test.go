// Copyright 2018-2019 opcua authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package ua

import (
	"testing"
	"time"
)

func TestCreateSubscriptionRequest(t *testing.T) {
	cases := []CodecTestCase{
		{
			Name: "normal",
			Struct: &CreateSubscriptionRequest{
				RequestHeader: &RequestHeader{
					AuthenticationToken: NewOpaqueNodeID(0, []byte{
						0xfe, 0x8d, 0x87, 0x79, 0xf7, 0x03, 0x27, 0x77,
						0xc5, 0x03, 0xa1, 0x09, 0x50, 0x29, 0x27, 0x60,
					}),
					AuditEntryID:  "",
					RequestHandle: 1003429,
					TimeoutHint:   10000,
					AdditionalHeader: &AdditionalHeader{
						TypeID:       NewTwoByteExpandedNodeID(0),
						EncodingMask: 0x00,
					},
					Timestamp: time.Date(2018, time.August, 10, 23, 0, 0, 0, time.UTC),
				},
				RequestedPublishingInterval: 500,
				RequestedLifetimeCount:      2400,
				RequestedMaxKeepAliveCount:  10,
				MaxNotificationsPerPublish:  65536,
				PublishingEnabled:           true,
				Priority:                    0,
			},
			Bytes: []byte{
				0x05, 0x00, 0x00, 0x10,
				0x00, 0x00, 0x00, 0xfe, 0x8d, 0x87, 0x79, 0xf7,
				0x03, 0x27, 0x77, 0xc5, 0x03, 0xa1, 0x09, 0x50,
				0x29, 0x27, 0x60, 0x00, 0x98, 0x67, 0xdd, 0xfd,
				0x30, 0xd4, 0x01, 0xa5, 0x4f, 0x0f, 0x00, 0x00,
				0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0x10,
				0x27, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x40, 0x7f, 0x40, 0x60, 0x09,
				0x00, 0x00, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x01, 0x00, 0x01, 0x00,
			},
		},
	}
	RunCodecTest(t, cases)
}
