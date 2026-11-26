package core

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// toolFiles are files whose presence shows a tool is in use, even without a
// matching package.json entry.
var toolFiles = map[string]string{
	"pnpm-lock.yaml": "pnpm", "pnpm-workspace.yaml": "pnpm", "bun.lock": "bun", "bun.lockb": "bun",
	"turbo.json": "turbo", "components.json": "shadcn", "nx.json": "nx",
	"wrangler.toml": "wrangler", "wrangler.json": "wrangler", "wrangler.jsonc": "wrangler",
	"netlify.toml": "netlify-cli", "vercel.json": "vercel", "firebase.json": "firebase",
}

// toolConfigs are config file prefixes ("vite.config.ts" -> vite).
var toolConfigs = map[string]string{
	"vite.config.": "vite", "vitest.config.": "vitest", "jest.config.": "jest", "playwright.config.": "@playwright/test",
	"cypress.config.": "cypress", "webpack.config.": "webpack", "tailwind.config.": "tailwindcss", "next.config.": "next",
	"nuxt.config.": "nuxt", "svelte.config.": "svelte", "drizzle.config.": "drizzle-orm", "eslint.config.": "eslint",
}

// ProjectDependencies maps JavaScript packages and tools used by the project to
// the file that shows it (package.json dependencies, lock and config files).
// It returns nil when the project has no package.json.
func ProjectDependencies(d string) map[string]string {
	files := scanFiles(d)
	deps := map[string]string{}
	js := false
	for _, f := range files {
		base := path.Base(f)
		if tool, ok := toolFiles[base]; ok {
			setDep(deps, tool, f)
		}
		for prefix, tool := range toolConfigs {
			if strings.HasPrefix(base, prefix) {
				setDep(deps, tool, f)
			}
		}
		if base != "package.json" {
			continue
		}
		js = true
		b, err := os.ReadFile(filepath.Join(d, filepath.FromSlash(f)))
		if err != nil || len(b) > 2_000_000 {
			continue
		}
		var pkg struct {
			PackageManager string            `json:"packageManager"`
			Deps           map[string]string `json:"dependencies"`
			Dev            map[string]string `json:"devDependencies"`
			Peer           map[string]string `json:"peerDependencies"`
			Optional       map[string]string `json:"optionalDependencies"`
		}
		if json.Unmarshal(b, &pkg) != nil {
			continue
		}
		for _, m := range []map[string]string{pkg.Deps, pkg.Dev, pkg.Peer, pkg.Optional} {
			for name := range m {
				setDep(deps, name, f)
			}
		}
		if manager, _, _ := strings.Cut(pkg.PackageManager, "@"); manager != "" {
			setDep(deps, manager, f)
		}
	}
	if !js {
		return nil
	}
	return deps
}

// setDep keeps the shallowest file as evidence.
func setDep(deps map[string]string, name, file string) {
	if old, ok := deps[name]; !ok || strings.Count(file, "/") < strings.Count(old, "/") {
		deps[name] = file
	}
}

// toolSkills maps the first segment of a skill name to the packages that show
// the project uses that tool. Such a skill only fits a JavaScript project that
// depends on one of them.
var toolSkills = map[string][]string{
	"vite": {"vite"}, "vitest": {"vitest"}, "jest": {"jest"}, "playwright": {"@playwright/test", "playwright"},
	"cypress": {"cypress"}, "prisma": {"prisma", "@prisma/client"}, "drizzle": {"drizzle-orm", "drizzle-kit"},
	"storybook": {"storybook", "@storybook/*"}, "tailwind": {"tailwindcss"}, "tailwindcss": {"tailwindcss"},
	"eslint": {"eslint"}, "biome": {"@biomejs/biome"}, "turborepo": {"turbo"}, "turbo": {"turbo"},
	"pnpm": {"pnpm"}, "bun": {"bun"}, "nx": {"nx"}, "webpack": {"webpack"}, "shadcn": {"shadcn"},
	"pinia": {"pinia"}, "nuxt": {"nuxt"}, "next": {"next"}, "nextjs": {"next"}, "redux": {"redux", "@reduxjs/toolkit"},
	"zustand": {"zustand"}, "tanstack": {"@tanstack/*"}, "trpc": {"@trpc/server", "@trpc/client"},
	"expo": {"expo"}, "svelte": {"svelte"}, "sveltekit": {"@sveltejs/kit"}, "angular": {"@angular/core"},
	"remix": {"@remix-run/*"}, "astro": {"astro"}, "hono": {"hono"}, "nestjs": {"@nestjs/core"},
	"express": {"express"}, "fastify": {"fastify"}, "mongoose": {"mongoose"}, "graphql": {"graphql"},
	"cloudflare": {"wrangler", "@cloudflare/*"}, "workers": {"wrangler", "@cloudflare/*"},
	"supabase": {"@supabase/supabase-js", "@supabase/*", "supabase"}, "firebase": {"firebase", "firebase-admin"},
	"netlify": {"netlify-cli", "@netlify/*"},
}

// ToolUse describes the JavaScript tools a skill name refers to.
type ToolUse struct {
	Used    []string // "next in apps/web/package.json"
	Missing []string // tool name segments the project does not use, e.g. "cloudflare"
}

// ToolEvidence checks every segment of a skill name that names a JavaScript
// tool ("nextjs-on-cloudflare": nextjs, cloudflare) against the project's
// dependencies. It returns a zero ToolUse for non-JavaScript projects.
func ToolEvidence(name string, deps map[string]string) ToolUse {
	var u ToolUse
	if deps == nil {
		return u
	}
	for _, seg := range strings.Split(name, "-") {
		pkgs, ok := toolSkills[seg]
		if !ok {
			continue
		}
		if dep, file := findDep(deps, pkgs); dep != "" {
			u.Used = append(u.Used, dep+" in "+file)
		} else {
			u.Missing = append(u.Missing, seg)
		}
	}
	return u
}

func findDep(deps map[string]string, pkgs []string) (string, string) {
	for _, p := range pkgs {
		if scope, wild := strings.CutSuffix(p, "*"); wild {
			for d, f := range deps {
				if strings.HasPrefix(d, scope) {
					return d, f
				}
			}
		} else if f, ok := deps[p]; ok {
			return p, f
		}
	}
	return "", ""
}
