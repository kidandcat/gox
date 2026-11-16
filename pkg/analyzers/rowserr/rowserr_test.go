package rowserr_test

import (
	"testing"

	"github.com/kidandcat/gox/pkg/analyzer"
	"github.com/kidandcat/gox/pkg/analyzer/analyzertest"
)

func TestRowsErr_missing(t *testing.T) {
	const src = `package p
import "database/sql"
func _(db *sql.DB) error {
	rows, err := db.Query("q")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x int
		if err := rows.Scan(&x); err != nil {
			return err
		}
	}
	return nil
}`
	issues := analyzertest.Run(t, get(), src)
	analyzertest.AssertLines(t, issues, []int{4})
}

func TestRowsErr_checkedOrEscapingIsFine(t *testing.T) {
	const src = `package p
import "database/sql"
func a(db *sql.DB) error {
	rows, err := db.Query("q")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}
func scanAll(r *sql.Rows) error { return r.Err() }
func b(db *sql.DB) error {
	rows, err := db.Query("q")
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	return scanAll(rows)
}
func c(db *sql.DB) (*sql.Rows, error) {
	rows, err := db.Query("q")
	return rows, err
}`
	analyzertest.AssertNone(t, analyzertest.Run(t, get(), src))
}

func get() *analyzer.Analyzer {
	for _, a := range analyzer.All() {
		if a.Name == "rowserr" {
			return a
		}
	}
	panic("rowserr analyzer not registered")
}
