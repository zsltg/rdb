package encoder

import "github.com/zsltg/rdb/core"

// Encoder is used to generate RDB file
type Encoder = core.Encoder

// NewEncoder creates an encoder instance
var NewEncoder = core.NewEncoder

// WithTTL specific expiration timestamp for object
var WithTTL = core.WithTTL
