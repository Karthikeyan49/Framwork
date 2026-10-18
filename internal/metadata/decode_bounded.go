package metadata

// DecodeJSONBounded is DecodeJSON with a caller-chosen input bound in bytes. Every
// other rule is identical and shared, not copied: invalid UTF-8, NUL, duplicate keys
// and keys that differ only by Unicode case folding, trailing data, nesting deeper
// than 32, more than 16000 nodes and inexact numbers are all rejected. Packages that
// decode a document which is not a metadata source (an AI proposal, a provider
// envelope) use it so that one strict decoder defines "ambiguous JSON".
//
// Rejections are *Invalid (Issues[0].Code is one of invalid_json, complexity_limit,
// duplicate_key, trailing_json, unsupported_character, number_limit) or ErrInvalid.
func DecodeJSONBounded(raw []byte, maxBytes int) (any, error) { return decodeJSON(raw, maxBytes) }
