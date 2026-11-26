package core

// accessWrite is not checked on Windows: the read-only attribute does not stop
// creating files in a directory, and ACLs need a real write to test.
func accessWrite(dir string) error { return nil }
