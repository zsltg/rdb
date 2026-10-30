package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/zsltg/rdb/model"
)

// encodeRDB writes one object with the encoder and returns the RDB bytes.
func encodeRDB(t *testing.T, enc *Encoder, buf *bytes.Buffer, write func(*Encoder) error) []byte {
	t.Helper()
	if err := enc.WriteHeader(); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteDBHeader(0, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := write(enc); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteEnd(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encoded(t *testing.T, write func(*Encoder) error) []byte {
	buf := &bytes.Buffer{}
	return encodeRDB(t, NewEncoder(buf), buf, write)
}

// strEnc returns a string with a 6-bit length prefix.
func strEnc(b []byte) []byte {
	return append([]byte{byte(len(b))}, b...)
}

// listPackOf builds a listpack of n small integers.
func listPackOf(n int) []byte {
	body := make([]byte, 0, 2*n)
	for i := 0; i < n; i++ {
		body = append(body, byte(i%100), 1) // 7-bit integer and its back length
	}
	head := make([]byte, 6)
	binary.LittleEndian.PutUint32(head, uint32(6+len(body)+1))
	binary.LittleEndian.PutUint16(head[4:], uint16(n))
	return append(append(head, body...), 0xff)
}

// zipMapOf builds a zipmap of n pairs. A counted zipmap has 255 in the first byte.
func zipMapOf(n int, counted bool) []byte {
	first := byte(n)
	if counted {
		first = 255
	}
	buf := []byte{first}
	for i := 0; i < n; i++ {
		buf = append(buf, 1, byte('a'+i%26)) // field
		buf = append(buf, 1, 0, 'v')         // value and free byte count
	}
	return append(buf, 0xff)
}

func plainRDB(typeCode byte, body ...[]byte) []byte {
	return rdbWith(typeCode, body...)
}

func repeat(n int, piece []byte) []byte {
	return bytes.Repeat(piece, n)
}

type maxElementsCase struct {
	name  string
	typ   string
	count int
	exact bool // the error shows the count of the whole key
	data  []byte
}

func maxElementsCases(t *testing.T) []maxElementsCase {
	member := []byte{1, 'm'}
	score := []byte{1, '1'}
	vals := func(n int) map[string][]byte {
		m := make(map[string][]byte, n)
		for i := 0; i < n; i++ {
			m[fmt.Sprintf("f%d", i)] = []byte("v")
		}
		return m
	}
	bigValues := func(n int) [][]byte {
		out := make([][]byte, n)
		for i := range out {
			out[i] = bytes.Repeat([]byte{byte('a' + i)}, 1500)
		}
		return out
	}
	zsetEntries := func(n int) []*model.ZSetEntry {
		out := make([]*model.ZSetEntry, n)
		for i := range out {
			out[i] = &model.ZSetEntry{Member: fmt.Sprintf("m%d", i), Score: float64(i)}
		}
		return out
	}
	// consumerPending moves the pending entries from the group to one consumer.
	stream := func(version uint, msgs, pending int, consumerPending bool) *model.StreamObject {
		id := &model.StreamId{Ms: 100, Sequence: 0}
		entry := &model.StreamEntry{FirstMsgId: id, Fields: []string{"a"}}
		for i := 0; i < msgs; i++ {
			entry.Msgs = append(entry.Msgs, &model.StreamMessage{
				Id:     &model.StreamId{Ms: 100, Sequence: uint64(i)},
				Fields: map[string]string{"a": "1"},
			})
		}
		s := &model.StreamObject{Version: version, Length: uint64(msgs), LastId: id}
		if msgs > 0 {
			s.Entries = []*model.StreamEntry{entry}
		}
		if pending > 0 {
			group := &model.StreamGroup{Name: "g", LastId: id}
			consumer := &model.StreamConsumer{Name: "c"}
			for i := 0; i < pending; i++ {
				ackId := &model.StreamId{Ms: 100, Sequence: uint64(i)}
				if consumerPending {
					consumer.Pending = append(consumer.Pending, ackId)
				} else {
					group.Pending = append(group.Pending, &model.StreamNAck{Id: ackId})
				}
			}
			if consumerPending {
				group.Consumers = []*model.StreamConsumer{consumer}
			}
			s.Groups = []*model.StreamGroup{group}
		}
		return s
	}
	zset2 := &bytes.Buffer{}
	zset2Data := encodeRDB(t, NewEncoder(zset2).SetZSetZipListOpt(64, 2), zset2, func(e *Encoder) error {
		return e.WriteZSetObject("k", zsetEntries(4))
	})
	valkey := &bytes.Buffer{}
	valkeyData := encodeRDB(t, NewEncoderValkey(valkey), valkey, func(e *Encoder) error {
		return e.WriteHashMapObjectEx("k", vals(4), map[string]int64{}, nil)
	})
	packed := append([]byte{2}, strEnc(listPackOf(3))...) // a packed node of three entries
	plain := []byte{1, 1, 'p'}                            // a plain node of one entry
	quick2Packed := bytes.Join([][]byte{{2}, packed, packed}, nil)
	quick2Plain := bytes.Join([][]byte{{3}, packed, packed, plain}, nil)

	return []maxElementsCase{
		{"list plain", model.ListType, 4, true, plainRDB(1, []byte{4}, repeat(4, member))},
		{"list ziplist", model.ListType, 4, true, encoded(t, func(e *Encoder) error {
			return e.WriteListObject("k", [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")})
		})},
		{"list quicklist", model.ListType, 6, false, encoded(t, func(e *Encoder) error {
			return e.WriteListObject("k", bigValues(6))
		})},
		{"list quicklist2 plain node", model.ListType, 7, false, plainRDB(18, quick2Plain)},
		{"list quicklist2 packed nodes", model.ListType, 6, false, plainRDB(18, quick2Packed)},
		{"set plain", model.SetType, 4, true, plainRDB(2, []byte{4}, repeat(4, member))},
		{"set intset", model.SetType, 4, true, encoded(t, func(e *Encoder) error {
			return e.WriteSetObject("k", [][]byte{[]byte("1"), []byte("2"), []byte("3"), []byte("4")})
		})},
		{"set listpack", model.SetType, 4, true, plainRDB(20, strEnc(listPackOf(4)))},
		{"hash plain", model.HashType, 4, true, plainRDB(4, []byte{4}, repeat(4, append(append([]byte{}, member...), member...)))},
		{"hash ziplist", model.HashType, 4, true, encoded(t, func(e *Encoder) error {
			return e.WriteHashMapObject("k", vals(4))
		})},
		{"hash zipmap", model.HashType, 4, true, plainRDB(9, strEnc(zipMapOf(4, false)))},
		{"hash zipmap counted", model.HashType, 4, true, plainRDB(9, strEnc(zipMapOf(4, true)))},
		{"hash listpack", model.HashType, 4, true, plainRDB(16, strEnc(listPackOf(8)))},
		{"hash with field expiration", model.HashType, 4, true, encoded(t, func(e *Encoder) error {
			return e.WriteHashMapObjectEx("k", vals(4), map[string]int64{})
		})},
		{"hash with field expiration valkey", model.HashType, 4, true, valkeyData},
		{"hash listpack with field expiration", model.HashType, 4, true, plainRDB(25, make([]byte, 8), strEnc(listPackOf(12)))},
		{"zset v1", model.ZSetType, 4, true, plainRDB(3, []byte{4}, repeat(4, append(append([]byte{}, member...), score...)))},
		{"zset v2", model.ZSetType, 4, true, zset2Data},
		{"zset ziplist", model.ZSetType, 4, true, encoded(t, func(e *Encoder) error {
			return e.WriteZSetObject("k", zsetEntries(4))
		})},
		{"zset listpack", model.ZSetType, 4, true, plainRDB(17, strEnc(listPackOf(8)))},
		{"stream entries", model.StreamType, 4, false, encoded(t, func(e *Encoder) error {
			return e.WriteStreamObject("k", stream(1, 4, 0, false))
		})},
		{"stream pending entries", model.StreamType, 4, false, encoded(t, func(e *Encoder) error {
			return e.WriteStreamObject("k", stream(2, 0, 4, false))
		})},
		{"stream consumer pending entries", model.StreamType, 4, false, encoded(t, func(e *Encoder) error {
			return e.WriteStreamObject("k", stream(3, 0, 4, true))
		})},
	}
}

func parseWithLimit(data []byte, limit int) (int, error) {
	dec := NewDecoder(bytes.NewReader(data)).WithMaxElements(limit)
	n := 0
	err := dec.Parse(func(model.RedisObject) bool {
		n++
		return true
	})
	return n, err
}

func TestWithMaxElements(t *testing.T) {
	for _, c := range maxElementsCases(t) {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseWithLimit(c.data, c.count-1)
			var tooMany *TooManyElementsError
			if !errors.Is(err, ErrTooManyElements) || !errors.As(err, &tooMany) {
				t.Fatalf("below the count: want ErrTooManyElements, got %v", err)
			}
			if tooMany.Key != "k" || tooMany.Type != c.typ || tooMany.Limit != c.count-1 || tooMany.Exact != c.exact {
				t.Fatalf("wrong fields: %+v", tooMany)
			}
			want := fmt.Sprintf(`too many elements in key "k": the %s has `, c.typ)
			if c.exact {
				want += fmt.Sprintf("%d elements and the limit is %d", c.count, c.count-1)
			} else {
				want += fmt.Sprintf("more than %d elements and the limit is %d", c.count-1, c.count-1)
			}
			if err.Error() != want {
				t.Fatalf("message:\n got %q\nwant %q", err.Error(), want)
			}
			if n, err := parseWithLimit(c.data, c.count); err != nil || n != 1 {
				t.Fatalf("at the count: want one object and no error, got %d, %v", n, err)
			}
			if n, err := parseWithLimit(c.data, 0); err != nil || n != 1 {
				t.Fatalf("no limit: want one object and no error, got %d, %v", n, err)
			}
		})
	}
}

func TestWithMaxElementsNegativeMeansNoLimit(t *testing.T) {
	data := plainRDB(2, []byte{4}, repeat(4, []byte{1, 'm'}))
	if n, err := parseWithLimit(data, -1); err != nil || n != 1 {
		t.Fatalf("want one object and no error, got %d, %v", n, err)
	}
}

// The limit applies to each key on its own.
func TestWithMaxElementsPerKey(t *testing.T) {
	data := encoded(t, func(e *Encoder) error {
		for _, k := range []string{"a", "b"} {
			if err := e.WriteSetObject(k, [][]byte{[]byte("x"), []byte("y"), []byte("z")}); err != nil {
				return err
			}
		}
		return nil
	})
	if n, err := parseWithLimit(data, 3); err != nil || n != 2 {
		t.Fatalf("want two objects and no error, got %d, %v", n, err)
	}
}

func TestWithMaxElementsNamesTheKey(t *testing.T) {
	data := encoded(t, func(e *Encoder) error {
		if err := e.WriteSetObject("small", [][]byte{[]byte("x")}); err != nil {
			return err
		}
		return e.WriteSetObject("big", [][]byte{[]byte("a"), []byte("b"), []byte("c")})
	})
	n, err := parseWithLimit(data, 2)
	var tooMany *TooManyElementsError
	if !errors.As(err, &tooMany) || tooMany.Key != "big" || n != 1 {
		t.Fatalf("want an error for key big after one object, got %d, %v", n, err)
	}
}

// A declared count must stop the decoder before it reads the elements.
func TestWithMaxElementsStopsBeforeReadingElements(t *testing.T) {
	// The list declares 2^40 elements and has no data.
	data := rdbWith(1, length64(1<<40))
	_, err := parseWithLimit(data, 10)
	var tooMany *TooManyElementsError
	if !errors.As(err, &tooMany) || !tooMany.Exact || tooMany.Count != 1<<40 {
		t.Fatalf("want an exact too many elements error, got %v", err)
	}
}
