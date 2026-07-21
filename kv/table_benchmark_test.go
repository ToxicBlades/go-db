package kv

import (
	"fmt"
	"testing"
)

func BenchmarkSecondaryIndexFind(b *testing.B) {
	table := benchmarkTable(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := table.Find("name", "name-500"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFullScan(b *testing.B) {
	table := benchmarkTable(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := table.Scan()
		if err != nil {
			b.Fatal(err)
		}
		for _, row := range rows {
			if row["name"] == "name-500" {
				break
			}
		}
	}
}

func benchmarkTable(b *testing.B) *Table {
	b.Helper()
	s, err := Open(b.TempDir() + "/db")
	if err != nil {
		b.Fatal(err)
	}
	table, err := NewTable(s, Schema{Columns: []Column{{Name: "id", Type: IntType}, {Name: "name", Type: StringType}}})
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if err := table.Insert(fmt.Sprint(i), Row{"id": i, "name": fmt.Sprintf("name-%d", i)}); err != nil {
			b.Fatal(err)
		}
	}
	b.Cleanup(func() { _ = table.Close() })
	return table
}
