package config

import "path/filepath"

const ACLDBFileName = "acl.sqlite"

// ResolveDBDir returns the ACL database directory.
// An empty dbPath defaults to {configDir}/db. Relative paths are resolved
// against the config file directory.
func ResolveDBDir(cfgPath, dbPath string) string {
	if dbPath == "" {
		return filepath.Join(filepath.Dir(cfgPath), "db")
	}
	if filepath.IsAbs(dbPath) {
		return dbPath
	}
	return filepath.Join(filepath.Dir(cfgPath), dbPath)
}

// ACLDBFile returns the SQLite file path inside dbDir.
func ACLDBFile(dbDir string) string {
	return filepath.Join(dbDir, ACLDBFileName)
}
