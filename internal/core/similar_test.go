package core

import "testing"

func TestSimilarSkills(t *testing.T) {
	rows := []IndexRow{
		{Name: "react-best-practices", Source: "vercel-labs/agent-skills", Description: "React and Next.js performance guidelines"},
		{Name: "vercel-react-best-practices", Source: "local", Description: "Apply Vercel React rules to projects"},
		{Name: "postgres", Source: "timescale/pg-aiguide", Description: "Any PostgreSQL database work"},
		{Name: "postgres-best-practices", Source: "neondatabase/postgres-skills", Description: "Best practices for Postgres"},
		{Name: "qdrant-scaling", Source: "qdrant/skills", Description: "Scale Qdrant clusters"},
		{Name: "qdrant-scaling-qps", Source: "qdrant/skills", Description: "Scale Qdrant for queries per second"},
		{Name: "qt-cpp-review", Source: "qt/agent-skills", Description: "Review Qt6 code with deterministic linting and deep analysis"},
		{Name: "qt-qml-review", Source: "qt/agent-skills", Description: "Review Qt6 code with deterministic linting and deep analysis"},
		{Name: "docs", Source: "a/b", Description: "Write documentation"},
		{Name: "duckdb-docs", Source: "duckdb/skills", Description: "DuckDB documentation"},
		{Name: "db-migrations", Source: "x/one", Description: "Plan reversible database schema migrations with rollback and review steps"},
		{Name: "migrations-db", Source: "y/two", Description: "Plan reversible database schema migrations with rollback and review steps"},
	}
	sim := SimilarSkills(rows, nil)
	pairs := map[string]bool{}
	for a, list := range sim {
		for _, s := range list {
			if a < s.Name {
				pairs[a+" ~ "+s.Name] = true
			}
		}
	}
	for _, want := range []string{"react-best-practices ~ vercel-react-best-practices", "postgres ~ postgres-best-practices", "db-migrations ~ migrations-db"} {
		if !pairs[want] {
			t.Errorf("missing %s in %v", want, pairs)
		}
	}
	for _, unwanted := range []string{"qdrant-scaling ~ qdrant-scaling-qps", "qt-cpp-review ~ qt-qml-review", "docs ~ duckdb-docs"} {
		if pairs[unwanted] {
			t.Errorf("false duplicate %s", unwanted)
		}
	}
	if got := SimilarSkills(rows, []string{"postgres"}); len(got) != 1 || got["postgres"][0].Name != "postgres-best-practices" {
		t.Fatalf("restricted: %v", got)
	}
}
