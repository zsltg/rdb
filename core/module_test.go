package core

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/zsltg/rdb/model"
)

const testModuleType = "test-type"

func TestModuleType(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := NewEncoder(buf)

	key := "testkey"
	expectedModuleEncVersion := uint64(42)
	expectedStrData := "testdata123"
	expectedUInt := uint64(123)

	err := enc.WriteHeader()
	if err != nil {
		t.Error(err)
		return
	}

	err = enc.WriteDBHeader(0, 1, 0)
	if err != nil {
		t.Error(err)
		return
	}
	err = enc.beforeWriteObject()
	if err != nil {
		t.Error(err)
		return
	}
	err = enc.write([]byte{typeModule2})
	if err != nil {
		t.Error(err)
		return
	}
	err = enc.writeString(key)
	if err != nil {
		t.Error(err)
		return
	}

	moduleId := createModuleId(testModuleType, expectedModuleEncVersion)
	err = enc.writeLength(moduleId)
	if err != nil {
		t.Error(err)
		return
	}
	err = enc.writeLength(uint64(ModuleOpcodeString))
	if err != nil {
		t.Error(err)
		return
	}
	err = enc.writeString(expectedStrData)
	if err != nil {
		t.Error(err)
		return
	}
	err = enc.writeLength(uint64(ModuleOpcodeUInt))
	if err != nil {
		t.Error(err)
		return
	}
	err = enc.writeLength(expectedUInt)
	if err != nil {
		t.Error(err)
		return
	}

	err = enc.writeLength(uint64(ModuleOpcodeEOF))
	if err != nil {
		t.Error(err)
		return
	}
	enc.state = writtenObjectState

	err = enc.WriteEnd()
	if err != nil {
		t.Error(err)
		return
	}
	data := buf.Bytes()
	t.Run("with parse", func(t *testing.T) {

		expectedResult := "expected-result"

		dec := NewDecoder(bytes.NewBuffer(data)).WithSpecialType(testModuleType,
			func(h ModuleTypeHandler, encVersion int) (interface{}, error) {
				if encVersion != 42 {
					t.Errorf("invalid encoding version, expected %d, actual %d",
						expectedModuleEncVersion, encVersion)
					return nil, fmt.Errorf("invalid encoding version, expected %d, actual %d",
						expectedModuleEncVersion, encVersion)
				}

				opcode, err := h.ReadOpcode()
				if err != nil {
					return nil, err
				}
				if opcode != ModuleOpcodeString {
					return nil, fmt.Errorf("invalid opcode read, expected %d (string), actual %d",
						ModuleOpcodeString, opcode)
				}
				data, err := h.ReadString()
				if err != nil {
					return nil, err
				}
				if !bytes.Equal(data, []byte(expectedStrData)) {
					return nil, fmt.Errorf("invalid string data read, expected %s, actual %s",
						expectedStrData, string(data))
				}

				opcode, err = h.ReadOpcode()
				if err != nil {
					return nil, err
				}
				if opcode != ModuleOpcodeUInt {
					return nil, fmt.Errorf("invalid opcode read, expected %d (uint), actual %d",
						ModuleOpcodeUInt, opcode)
				}
				val, err := h.ReadUInt()
				if err != nil {
					return nil, err
				}
				if val != expectedUInt {
					return nil, fmt.Errorf("invalid unsigned int read, expected %d, actual %d",
						expectedUInt, val)
				}
				opcode, err = h.ReadOpcode()
				if err != nil {
					return nil, err
				}
				if opcode != ModuleOpcodeEOF {
					return nil, fmt.Errorf("invalid opcode read, expected %d (EOF), actual %d",
						ModuleOpcodeEOF, opcode)
				}
				return expectedResult, nil
			})

		err = dec.Parse(func(o model.RedisObject) bool {
			if o.GetKey() != key {
				t.Errorf("invalid object key, expected %s, actual %s", key, o.GetKey())
				return false
			}
			if o.GetType() != testModuleType {
				t.Errorf("invalid redis type, expected %s, actual %s", testModuleType, o.GetType())
				return false
			}
			mtObj, ok := o.(*model.ModuleTypeObject)
			if !ok {
				t.Errorf("invalid object type, expected model.ModuleTypeObject")
				return false
			}

			if mtObj.Value != expectedResult {
				t.Errorf("invalid return value")
				return false
			}

			return true
		})
		if err != nil {
			t.Error(err)
		}
	})
	t.Run("skip parse", func(t *testing.T) {
		dec := NewDecoder(bytes.NewBuffer(data))
		err = dec.Parse(func(o model.RedisObject) bool {
			if o.GetKey() != key {
				t.Errorf("invalid object key, expected %s, actual %s", key, o.GetKey())
				return false
			}
			if o.GetType() != testModuleType {
				t.Errorf("invalid redis type, expected %s, actual %s", testModuleType, o.GetType())
				return false
			}
			mtObj, ok := o.(*model.ModuleTypeObject)
			if !ok {
				t.Errorf("invalid object type, expected model.ModuleTypeObject")
				return false
			}

			if mtObj.Value != nil {
				t.Errorf("invalid return value")
				return false
			}

			return true
		})
		if err != nil {
			t.Error(err)
		}
	})
}

func TestCorrectModuleTypeEncodeDecode(t *testing.T) {
	moduleId := createModuleId(testModuleType, 42)
	name := moduleTypeNameByID(moduleId)
	encVersion := moduleTypeEncVersionByID(moduleId)
	if name != testModuleType {
		t.Errorf("invalid module type name, expected %s, actual %s", testModuleType, name)
	}
	if encVersion != 42 {
		t.Errorf("invalid module encoding version, expected %d, actual %d", 42, encVersion)
	}
}

func createModuleId(moduleType string, encVersion uint64) uint64 {
	moduleId := uint64(0)
	for i := 0; i < 9; i++ {
		moduleId |= uint64(charCode(moduleType[i]))
		moduleId <<= 6
	}
	moduleId <<= 4
	moduleId |= encVersion
	return moduleId
}

func charCode(c uint8) uint8 {
	for i, csChar := range []byte(ModuleTypeNameCharSet) {
		if c == csChar {
			return uint8(i)
		}
	}
	panic(fmt.Errorf("unsupported char %c", c))
}

// captureStdout runs fn and returns what fn wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	func() {
		defer func() {
			os.Stdout = orig
			_ = w.Close()
		}()
		fn()
	}()
	out := <-done
	_ = r.Close()
	return out
}

// writeUnknownModule writes the module ID and a skippable payload.
func writeUnknownModule(t *testing.T, enc *Encoder) {
	t.Helper()
	mustNoErr(t, enc.writeLength(createModuleId("unknown-m", 1)))
	mustNoErr(t, enc.writeLength(uint64(ModuleOpcodeUInt)))
	mustNoErr(t, enc.writeLength(7))
	mustNoErr(t, enc.writeLength(uint64(ModuleOpcodeEOF)))
}

func TestUnknownModuleWritesNothingToStdout(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T, enc *Encoder)
		want  int
	}{
		{
			name: "aux opcode",
			build: func(t *testing.T, enc *Encoder) {
				mustNoErr(t, enc.write([]byte{opCodeModuleAux}))
				writeUnknownModule(t, enc)
			},
			want: 0,
		},
		{
			name: "module value type",
			build: func(t *testing.T, enc *Encoder) {
				mustNoErr(t, enc.beforeWriteObject())
				mustNoErr(t, enc.write([]byte{typeModule2}))
				mustNoErr(t, enc.writeString("testkey"))
				writeUnknownModule(t, enc)
				enc.state = writtenObjectState
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := bytes.NewBuffer(nil)
			enc := NewEncoder(buf)
			mustNoErr(t, enc.WriteHeader())
			if tt.want == 0 {
				tt.build(t, enc)
				mustNoErr(t, enc.WriteDBHeader(0, 0, 0))
				enc.state = writtenObjectState
			} else {
				mustNoErr(t, enc.WriteDBHeader(0, 1, 0))
				tt.build(t, enc)
			}
			mustNoErr(t, enc.WriteEnd())

			var objects int
			var parseErr error
			out := captureStdout(t, func() {
				parseErr = NewDecoder(bytes.NewReader(buf.Bytes())).Parse(func(o model.RedisObject) bool {
					objects++
					return true
				})
			})
			mustNoErr(t, parseErr)
			if objects != tt.want {
				t.Fatalf("objects: want %d, got %d", tt.want, objects)
			}
			if out != "" {
				t.Fatalf("stdout not empty: %q", out)
			}
		})
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
