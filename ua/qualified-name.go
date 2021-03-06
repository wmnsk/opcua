// Copyright 2018-2019 opcua authors. All rights reserved.
// Use of this source code is governed by a MIT-style license that can be
// found in the LICENSE file.

package ua

// QualifiedName contains a qualified name. It is, for example, used as BrowseName.
// The name part of the QualifiedName is restricted to 512 characters.
//
// Specification: Part 3, 8.3
type QualifiedName struct {
	NamespaceIndex uint16
	Name           string
}

// NewQualifiedName creates a new QualifiedName.
func NewQualifiedName(index uint16, name string) *QualifiedName {
	return &QualifiedName{
		NamespaceIndex: index,
		Name:           name,
	}
}

func (q *QualifiedName) String() string {
	return q.Name
}
