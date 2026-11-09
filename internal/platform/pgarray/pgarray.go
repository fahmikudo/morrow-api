// Package pgarray maps `text[]` and `int[]` columns to the Go slices the API
// exposes. The pgx stdlib driver hands arrays back in Postgres text format, so
// these types carry the Scanner/Valuer pair Gorm needs and never leak `null`.
package pgarray

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

var typeMap = pgtype.NewMap()

// TextArray is a `text[]` column value. The zero value is SQL NULL.
type TextArray struct {
	values []string
	valid  bool
}

// Texts builds a non-null `text[]`; nil and empty both encode as `{}`.
func Texts(values []string) TextArray {
	if values == nil {
		values = []string{}
	}
	return TextArray{values: values, valid: true}
}

// Strings returns the elements, never nil.
func (a TextArray) Strings() []string {
	if !a.valid || a.values == nil {
		return []string{}
	}
	return a.values
}

// Value encodes the Postgres array literal, or NULL for the zero value.
func (a TextArray) Value() (driver.Value, error) {
	if !a.valid {
		return nil, nil
	}
	return encodeText(a.values), nil
}

// Scan decodes the Postgres text-format array.
func (a *TextArray) Scan(src any) error {
	if src == nil {
		*a = TextArray{}
		return nil
	}

	raw, err := asBytes(src)
	if err != nil {
		return err
	}

	var flat pgtype.FlatArray[string]
	if err := typeMap.Scan(pgtype.TextArrayOID, pgtype.TextFormatCode, raw, &flat); err != nil {
		return err
	}

	*a = TextArray{values: []string(flat), valid: true}
	return nil
}

// GormDataType tells Gorm this is a column, not a relation.
func (TextArray) GormDataType() string {
	return "text[]"
}

// IntArray is an `int[]` column value. The zero value is SQL NULL.
type IntArray struct {
	values []int32
	valid  bool
}

// GormDataType tells Gorm this is a column, not a relation.
func (IntArray) GormDataType() string {
	return "int[]"
}

// Ints builds a non-null `int[]`; nil and empty both encode as `{}`.
func Ints(values []int32) IntArray {
	if values == nil {
		values = []int32{}
	}
	return IntArray{values: values, valid: true}
}

// Values returns the elements, never nil.
func (a IntArray) Values() []int32 {
	if !a.valid || a.values == nil {
		return []int32{}
	}
	return a.values
}

// Value encodes the Postgres array literal, or NULL for the zero value.
func (a IntArray) Value() (driver.Value, error) {
	if !a.valid {
		return nil, nil
	}
	return encodeInts(a.values), nil
}

// Scan decodes the Postgres text-format array.
func (a *IntArray) Scan(src any) error {
	if src == nil {
		*a = IntArray{}
		return nil
	}

	raw, err := asBytes(src)
	if err != nil {
		return err
	}

	var flat pgtype.FlatArray[int32]
	if err := typeMap.Scan(pgtype.Int4ArrayOID, pgtype.TextFormatCode, raw, &flat); err != nil {
		return err
	}

	*a = IntArray{values: []int32(flat), valid: true}
	return nil
}

func asBytes(src any) ([]byte, error) {
	switch value := src.(type) {
	case string:
		return []byte(value), nil
	case []byte:
		return value, nil
	default:
		return nil, fmt.Errorf("pgarray: unsupported source %T", src)
	}
}

func encodeText(values []string) string {
	if len(values) == 0 {
		return "{}"
	}

	var encoded strings.Builder
	encoded.WriteByte('{')
	for index, value := range values {
		if index > 0 {
			encoded.WriteByte(',')
		}

		encoded.WriteByte('"')
		for _, character := range value {
			if character == '"' || character == '\\' {
				encoded.WriteByte('\\')
			}
			encoded.WriteRune(character)
		}
		encoded.WriteByte('"')
	}
	encoded.WriteByte('}')

	return encoded.String()
}

func encodeInts(values []int32) string {
	if len(values) == 0 {
		return "{}"
	}

	var encoded strings.Builder
	encoded.WriteByte('{')
	for index, value := range values {
		if index > 0 {
			encoded.WriteByte(',')
		}
		encoded.WriteString(strconv.FormatInt(int64(value), 10))
	}
	encoded.WriteByte('}')

	return encoded.String()
}
