// Copyright 2015 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rafthttp

import (
	"encoding/binary"
	"io"

	"bitbucket.org/bestchai/dinv/dinvRT"
	"github.com/coreos/etcd/pkg/pbutil"
	"github.com/coreos/etcd/raft/raftpb"
)

// messageEncoder is a encoder that can encode all kinds of messages.
// It MUST be used with a paired messageDecoder.
type messageEncoder struct {
	w io.Writer
}

var dinv = false

func (enc *messageEncoder) encode(m *raftpb.Message) error {
	if dinv {
		buf := pbutil.MustMarshal(m)
		ibuf := dinvRT.Pack(buf)
		if err := binary.Write(enc.w, binary.BigEndian, uint64(len(ibuf))); err != nil {
			return err
		}
		_, err := enc.w.Write(ibuf)
		return err
	} else {
		if err := binary.Write(enc.w, binary.BigEndian, uint64(m.Size())); err != nil {
			return err
		}
		_, err := enc.w.Write(pbutil.MustMarshal(m))
		return err
	}
}

// messageDecoder is a decoder that can decode all kinds of messages.
type messageDecoder struct {
	r io.Reader
}

func (dec *messageDecoder) decode() (raftpb.Message, error) {
	var m raftpb.Message
	var l uint64

	if err := binary.Read(dec.r, binary.BigEndian, &l); err != nil {
		return m, err
	}
	buf := make([]byte, int(l))

	if _, err := io.ReadFull(dec.r, buf); err != nil {
		return m, err
	}
	if dinv {
		var ibuf []byte
		dinvRT.Unpack(buf, &ibuf)
		return m, m.Unmarshal(ibuf)
	} else {
		return m, m.Unmarshal(buf)
	}
}
