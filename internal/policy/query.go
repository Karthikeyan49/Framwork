package policy

// SQLFieldActions is a closed proof for the built-in action kernels. These
// concrete implementations express row-dependent field visibility in DataScope,
// not opaque Go action callbacks. Unknown wrappers must use bounded trusted-row
// action checks; embedding Native does not inherit this proof.
func SQLFieldActions(engine Engine) bool {
	switch e := engine.(type) {
	case Native, Defined:
		return true
	case *Native:
		return e != nil
	case *Defined:
		return e != nil
	default:
		return false
	}
}
