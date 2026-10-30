package core

import (
	"errors"
	"fmt"
)

// ErrTooManyElements is returned, wrapped in a *TooManyElementsError, when a
// key has more elements than the limit set with Decoder.WithMaxElements.
var ErrTooManyElements = errors.New("too many elements")

// TooManyElementsError describes a key that is over the element limit.
// It never holds element values.
type TooManyElementsError struct {
	// Key is the name of the key.
	Key string
	// Type is the model type of the key: list, set, hash, zset or stream.
	Type string
	// Count is the number of elements that the decoder counted. If Exact
	// is false, the key can have more elements, because the decoder stopped
	// early.
	Count uint64
	// Limit is the value that was set with WithMaxElements.
	Limit int
	// Exact is true if Count is the number of elements of the whole key.
	Exact bool
}

// Error returns the message of the error.
func (e *TooManyElementsError) Error() string {
	if e.Exact {
		return fmt.Sprintf("%s in key %q: the %s has %d elements and the limit is %d",
			ErrTooManyElements, e.Key, e.Type, e.Count, e.Limit)
	}
	return fmt.Sprintf("%s in key %q: the %s has more than %d elements and the limit is %d",
		ErrTooManyElements, e.Key, e.Type, e.Limit, e.Limit)
}

// Unwrap lets errors.Is match ErrTooManyElements.
func (e *TooManyElementsError) Unwrap() error {
	return ErrTooManyElements
}

// WithMaxElements sets the largest number of elements that the decoder reads
// for one list, set, hash, sorted set or stream. A key above the limit makes
// Parse return a *TooManyElementsError before the decoder allocates its
// elements. A value of 0 or less means no limit, which is the default.
func (dec *Decoder) WithMaxElements(n int) *Decoder {
	if n < 0 {
		n = 0
	}
	dec.maxElements = uint64(n)
	return dec
}

// beginKey resets the element count for a new key.
func (dec *Decoder) beginKey(key string) {
	dec.elementKey = key
	dec.elementSeen = 0
}

// countElements adds n elements of the current key to the running count and
// returns an error if the count is over the limit. If whole is true, n is the
// element count of the whole key, so the error shows an exact count.
func (dec *Decoder) countElements(typ string, n uint64, whole bool) error {
	if dec.maxElements == 0 {
		return nil
	}
	// elementSeen never passes maxElements here, so the subtraction is safe.
	if n <= dec.maxElements-dec.elementSeen {
		dec.elementSeen += n
		return nil
	}
	count := n
	if !whole {
		count = dec.elementSeen + n
		if count < n { // overflow
			count = n
		}
	}
	return &TooManyElementsError{
		Key:   dec.elementKey,
		Type:  typ,
		Count: count,
		Limit: int(dec.maxElements),
		Exact: whole,
	}
}
