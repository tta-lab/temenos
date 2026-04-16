package sandbox

// isSubdirOf checks if child starts with parent + "/".
func isSubdirOf(child, parent string) bool {
	return len(child) > len(parent) && child[:len(parent)+1] == parent+"/"
}
