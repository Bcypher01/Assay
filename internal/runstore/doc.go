// Package runstore persists run history and supports comparison between runs.
// SQLite holds queryable metadata while immutable study, result, and trade
// artifacts remain alongside it for portability and efficient bulk storage.
package runstore
