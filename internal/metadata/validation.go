package metadata

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"

	contracts "github.com/Karthikeyan49/Framwork/contracts/jsonschema"
	js "github.com/santhosh-tekuri/jsonschema/v6"
)

var ErrInvalid = errors.New("invalid metadata")

type Issue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Invalid struct {
	Issues []Issue `json:"issues"`
}

func (e *Invalid) Error() string               { return ErrInvalid.Error() }
func (e *Invalid) Unwrap() error               { return ErrInvalid }
func invalid(path, code, message string) error { return &Invalid{[]Issue{{path, code, message}}} }

// Source is minted only after schema and structural validation. Its bytes cannot
// be modified by consumers. Hash identifies source storage, never compiler IR.
type Source struct {
	document Document
	raw      []byte
	hash     string
}

func (s Source) AppID() string         { return s.document.AppID }
func (s Source) Namespace() string     { return s.document.Namespace }
func (s Source) Hash() string          { return s.hash }
func (s Source) JSON() json.RawMessage { return append(json.RawMessage(nil), s.raw...) }

type Validator struct{ schema *js.Schema }
type noLoader struct{}

func (noLoader) Load(string) (any, error) {
	return nil, errors.New("external schema loading is disabled")
}
func NewValidator() (*Validator, error) {
	raw, err := contracts.Files.ReadFile("application-v1.schema.json")
	if err != nil {
		return nil, err
	}
	schema, err := js.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := js.NewCompiler()
	c.DefaultDraft(js.Draft2020)
	c.UseLoader(noLoader{})
	if err = c.AddResource("urn:framework:metadata:1.0.0", schema); err != nil {
		return nil, err
	}
	compiled, err := c.Compile("urn:framework:metadata:1.0.0")
	if err != nil {
		return nil, err
	}
	return &Validator{compiled}, nil
}
func (v *Validator) Parse(raw []byte) (Source, error) { return v.parse(raw, MaxBytes) }

// Persisted JSONB text may contain whitespace added by PostgreSQL. It has a
// separate storage bound; the validated normalized source still obeys MaxBytes.
func (v *Validator) parse(raw []byte, inputLimit int) (Source, error) {
	if v == nil || v.schema == nil {
		return Source{}, ErrUnavailable
	}
	if len(raw) > inputLimit {
		return Source{}, invalid("", "size_limit", "Metadata exceeds 256 KiB")
	}
	value, err := decodeJSON(raw, inputLimit)
	if err != nil {
		return Source{}, err
	}
	if err = v.schema.Validate(value); err != nil {
		var validation *js.ValidationError
		if errors.As(err, &validation) {
			return Source{}, invalid("/"+strings.Join(validation.InstanceLocation, "/"), "schema_violation", "Document violates metadata schema 1.0.0")
		}
		return Source{}, ErrInvalid
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return Source{}, ErrInvalid
	}
	var document Document
	if err = json.Unmarshal(normalized, &document); err != nil {
		return Source{}, ErrInvalid
	}
	seen := map[string]bool{}
	for i, d := range document.Definitions {
		path := fmt.Sprintf("/definitions/%d", i)
		if seen[d.ID] {
			return Source{}, invalid(path+"/id", "duplicate_id", "Definition IDs must be unique")
		}
		seen[d.ID] = true
		if d.Namespace != document.Namespace {
			return Source{}, invalid(path+"/namespace", "namespace_mismatch", "Definition namespace must match its envelope")
		}
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return Source{}, ErrInvalid
	}
	// Ensure Go serialization preserves the published contract, including empty
	// required arrays/maps and numeric spellings normalized for JSONB persistence.
	roundTrip, err := DecodeJSON(encoded)
	if err != nil || v.schema.Validate(roundTrip) != nil {
		return Source{}, ErrInvalid
	}
	if len(encoded) > MaxBytes {
		return Source{}, invalid("", "size_limit", "Normalized metadata exceeds 256 KiB")
	}
	digest := sha256.Sum256(encoded)
	return Source{document, encoded, hex.EncodeToString(digest[:])}, nil
}

// DecodeJSON rejects duplicate keys, invalid UTF-8, trailing data and excessive
// nesting before schema validation. Numbers are exact decimal values with at most
// 18 fractional digits and bounded exponents; no floating-point hash conversion.
func DecodeJSON(raw []byte) (any, error) { return decodeJSON(raw, MaxBytes+4096) }
func decodeJSON(raw []byte, maxBytes int) (any, error) {
	if len(raw) == 0 || len(raw) > maxBytes || !utf8.Valid(raw) {
		return nil, invalid("", "invalid_json", "Invalid or oversized JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	nodes := 0
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		nodes++
		if depth > 32 || nodes > 16000 {
			return nil, invalid("", "complexity_limit", "JSON exceeds nesting or node limits")
		}
		token, err := dec.Token()
		if err != nil {
			return nil, ErrInvalid
		}
		switch token := token.(type) {
		case json.Delim:
			if token == '{' {
				values := map[string]any{}
				for dec.More() {
					key, err := dec.Token()
					if err != nil {
						return nil, ErrInvalid
					}
					name, ok := key.(string)
					if !ok || strings.ContainsRune(name, 0) {
						return nil, ErrInvalid
					}
					if _, exists := values[name]; exists {
						return nil, invalid("", "duplicate_key", "Duplicate JSON keys are forbidden")
					}
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					values[name] = value
				}
				end, err := dec.Token()
				if err != nil || end != json.Delim('}') {
					return nil, ErrInvalid
				}
				return values, nil
			}
			if token == '[' {
				values := []any{}
				for dec.More() {
					value, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					values = append(values, value)
				}
				end, err := dec.Token()
				if err != nil || end != json.Delim(']') {
					return nil, ErrInvalid
				}
				return values, nil
			}
			return nil, ErrInvalid
		case string:
			if strings.ContainsRune(token, 0) {
				return nil, invalid("", "unsupported_character", "NUL is not supported in persisted metadata")
			}
			return token, nil
		case json.Number:
			spelling := token.String()
			if len(spelling) > 64 {
				return nil, ErrInvalid
			}
			if at := strings.IndexAny(spelling, "eE"); at >= 0 {
				exponent, err := strconv.Atoi(spelling[at+1:])
				if err != nil || exponent < -18 || exponent > 18 {
					return nil, invalid("", "number_limit", "Numeric exponent exceeds supported bounds")
				}
			}
			number, ok := new(big.Rat).SetString(spelling)
			if !ok {
				return nil, ErrInvalid
			}
			scaled := new(big.Rat).Mul(number, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)))
			if !scaled.IsInt() {
				return nil, invalid("", "number_limit", "Numbers support at most 18 decimal places")
			}
			if number.IsInt() {
				return json.Number(number.Num().String()), nil
			}
			canonical := strings.TrimRight(strings.TrimRight(number.FloatString(18), "0"), ".")
			return json.Number(canonical), nil
		default:
			return token, nil
		}
	}
	value, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err = dec.Token(); err != io.EOF {
		return nil, invalid("", "trailing_json", "Exactly one JSON document is required")
	}
	return value, nil
}
