#!/usr/bin/env python3
"""Generate packs/*.yaml: curated stack packs + auto vendor packs (packs/vendor/<owner>.yaml).

Usage: build-packs.py [--force] [--vendors-only]
  Stack packs are written only if the file does not exist yet (your edits win) unless --force.
  Vendor packs (one per upstream owner with >= 3 skills) are always regenerated.
"""
import collections, re, sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import skillcore as sc  # noqa: E402

STACK = {
    "core": ("Baseline engineering workflow for any project (keep small: loaded every session)", [],
             "tdd diagnosing-bugs verification-before-completion review-pr planning-with-files investigate-codebase"),
    "web-vue": ("Vue 3 / Nuxt / Pinia / Vite stack", [],
                "vue vue-best-practices vue-router-best-practices vue-pinia-best-practices vue-testing-best-practices "
                "vue-debug-guides create-adaptable-composable nuxt nitro pinia vite vitest unocss pnpm"),
    "web-react": ("React / Next.js / shadcn stack", [],
                  "react-best-practices composition-patterns shadcn vite vitest pnpm turborepo"),
    "web-svelte": ("Svelte / SvelteKit", [], "svelte-code-writer svelte-core-bestpractices vite vitest"),
    "web-quality": ("Frontend quality: UI guidelines, web perf, browser testing", [],
                    "web-design-guidelines web-perf webapp-testing playwright-cli"),
    "docs-site": ("Docs sites, slides and user docs", [], "vitepress slidev writing-user-docs document-api-endpoint"),
    "python": ("Modern Python projects and packaging", [],
               "modern-python python-pypi-package-builder property-based-testing fastapi"),
    "data-science": ("Notebook-style analysis: sklearn, polars, duckdb, plots", [],
                     "scikit-learn polars python-data-viz query read-file attach-db"),
    "ml-train": ("Model training / fine-tuning (PyTorch, Hugging Face, TRL, Unsloth)", [],
                 "ml-python hf-cli huggingface-best huggingface-datasets huggingface-llm-trainer trl-training unsloth "
                 "experiment-tracking profile-model debug-graph-breaks"),
    "ml-tracking": ("Experiment tracking + LLM eval/tracing (W&B, Weave, MLflow, Trackio)", [],
                    "wandb-primary weave-instrument mlflow-onboarding mlflow-evaluation mlflow-trace-instrumentation huggingface-trackio"),
    "vision": ("Computer vision (YOLO / Ultralytics, HF vision trainer)", [],
               "yolo yolo-training yolo-datasets yolo-inference yolo-export huggingface-vision-trainer"),
    "ml-serve": ("Model serving: vLLM, BentoML, local models", [],
                 "vllm-deploy-simple vllm-deploy-docker vllm-deploy-k8s vllm-bench-serve bentoml-create-bento "
                 "bentoml-containerize huggingface-local-models hf-cloud-serving-image-selection"),
    "llm-apps": ("LLM apps / agents / RAG (Claude API, MCP, LangChain, vector DBs)", [],
                 "claude-api mcp-builder langchain-build-agent langchain-agent-tracing langsmith-trace langsmith-evaluator "
                 "llamaparse qdrant-advisor qdrant-hybrid-search milvus pgvector-semantic-search redis-semantic-cache llm-security"),
    "data-eng": ("Data engineering: dbt, Airflow, Dagster, Kafka, ClickHouse, DuckDB", [],
                 "using-dbt-for-analytics-engineering running-dbt-commands adding-dbt-unit-test dagster-expert airflow "
                 "authoring-dags testing-dags debugging-dags developing-kafka-python-client kafka-schema-registry "
                 "clickhouse-best-practices query"),
    "node-backend": ("Node/TS backend: NestJS, Hono, Prisma, GraphQL", [],
                     "nestjs nestjs-testing nestjs-data nestjs-auth hono prisma-orm-setup prisma-client-api prisma-cli "
                     "apollo-server graphql-schema graphql-operations api-and-interface-design"),
    "jvm": ("Java / Kotlin backend (Spring Boot, Quarkus, JPA, Testcontainers)", [],
            "java-springboot kotlin-springboot spring-boot-testing java-testcontainers quarkus-application-development "
            "kotlin-backend-jpa-entity-mapping kotlin-tooling-java-to-kotlin"),
    "dotnet": (".NET / C# / EF Core", [], "dotnet-best-practices csharp-async ef-core"),
    "postgres": ("PostgreSQL design, migrations, search, tuning", [],
                 "postgres postgres-best-practices design-postgres-tables postgres-database-migration "
                 "postgres-hybrid-text-search pgvector-semantic-search sql-optimization schema-exploration"),
    "nosql": ("Redis, MongoDB, Neo4j", [],
              "redis-core redis-connections redis-search mongodb mongodb-schema-design mongodb-query-optimizer "
              "mongodb-connection neo4j-getting-started-skill neo4j-cypher-skill neo4j-modeling-skill"),
    "docker": ("Dockerfiles, Compose, build strategy, safe docker ops", [],
               "docker-project-foundations docker-build-strategies docker-compose-patterns multi-stage-dockerfile "
               "docker-destructive-guardrails docker-docs"),
    "iac": ("Infrastructure as code: Terraform + Pulumi", [],
            "terraform-style-guide terraform-test refactor-module terraform-search-import pulumi-best-practices "
            "pulumi-overview pulumi-debug-failed-operation pulumi-component"),
    "k8s-gitops": ("Kubernetes GitOps with Flux", [], "gitops-knowledge gitops-repo-audit gitops-cluster-debug"),
    "cloudflare": ("Cloudflare Workers / Wrangler / Durable Objects", [],
                   "cloudflare workers-best-practices workers-development wrangler durable-objects frameworks-on-cloudflare cf-cli"),
    "ci-github": ("GitHub Actions + PR workflow", [],
                  "github-actions github-actions-hardening gha-security-review gh-fix-ci iterate-pr pr-writer commit"),
    "security": ("Security review, SAST, hardening", [],
                 "security-hardening security-best-practices code-security semgrep variant-analysis find-bugs "
                 "secret-serialization security-model security-audit"),
    "mobile-rn": ("Expo / React Native", [],
                  "expo-overview expo-router expo-data-fetching expo-upgrade expo-native-ui expo-ui eas-update eas-workflows "
                  "react-native-best-practices react-native-testing react-navigation upgrading-react-native"),
    "flutter": ("Flutter / Dart", [],
                "flutter-apply-architecture-best-practices flutter-build-responsive-layout flutter-fix-layout-issues "
                "flutter-add-widget-test flutter-add-integration-test flutter-setup-declarative-routing "
                "flutter-implement-json-serialization flutter-use-http-package flutter-setup-localization "
                "dart-run-static-analysis dart-fix-runtime-errors dart-use-pattern-matching dart-add-unit-test"),
    "research-writing": ("Research papers: find, read, cite, write, polish", [],
                         "find-papers fetch-paper literature-review draft-related-work verify-citations verify-claims "
                         "write-abstract polish-prose polish-tables-figures technical-writing paper-profile huggingface-papers"),
    "office-docs": ("Office files: docx, pptx, xlsx, pdf, slides", [],
                    "docx pptx xlsx pdf documents spreadsheets presentations make-slides"),
    "web3": ("Smart contracts: Solidity, Solana, Sui (OpenZeppelin)", [],
             "develop-secure-contracts setup-solidity-contracts upgrade-solidity-contracts solana-dev "
             "review-sui-contracts setup-sui-contracts"),
    "qt": ("Qt / QML / C++", [], "qt-qml qt-qml-review qt-qml-test qt-cpp-review qt-cmake-project qt-ui-design"),
    "skill-authoring": ("Writing and reviewing agent skills", [], "skill-creator skill-writer skill-scanner review-skill"),
    # composite stacks
    "fullstack-vue": ("Vue frontend + Node backend + Postgres + Docker", ["web-vue", "node-backend", "postgres", "docker"], ""),
    "fullstack-react": ("React frontend + Node backend + Postgres + Docker", ["web-react", "node-backend", "postgres", "docker"], ""),
    "ml-project": ("End-to-end ML project: Python + training + tracking", ["python", "ml-train", "ml-tracking"], ""),
}


def main():
    force = "--force" in sys.argv
    lib = set(sc.library_names())
    sc.PACKS.mkdir(exist_ok=True)
    wrote = 0
    if "--vendors-only" not in sys.argv:
        for name, (desc, ext, skills) in STACK.items():
            if sc.pack_path(name).exists() and not force:
                continue
            sc.save_pack({"name": name, "description": desc, "extends": ext, "skills": skills.split()})
            wrote += 1
            absent = [s for s in skills.split() if s not in lib]
            if absent:
                print(f"note: @{name}: {len(absent)} skill(s) not in library yet: {', '.join(absent[:6])}")
    by_owner = collections.defaultdict(list)
    for n in sorted(lib):
        src = sc.load_json(sc.SKILLS / n / "SOURCE.json").get("repo")
        if src:
            by_owner[re.sub(r"[^a-z0-9-]+", "-", src.split("/")[0].lower()).strip("-")].append(n)
    vendor_dir = sc.PACKS / "vendor"
    keep = set()
    for owner, skills in by_owner.items():
        if len(skills) < 3:
            continue
        repos = sorted({sc.load_json(sc.SKILLS / s / "SOURCE.json")["repo"] for s in skills})
        sc.save_pack({"name": f"vendor/{owner}", "description": f"All library skills from {', '.join(repos)}",
                      "skills": skills})
        keep.add(f"{owner}.yaml")
    for p in vendor_dir.glob("*.yaml"):
        if p.name not in keep:
            p.unlink()
    stack_cov = {s for n in sc.list_packs() if not n.startswith("vendor/") for s in sc.load_pack(n)["skills"]}
    print(f"stack packs written: {wrote} · vendor packs: {len(keep)} · stack coverage {len(stack_cov)}/{len(lib)}")
    probs = [(n, m) for n, m in sc.validate_packs() if not m.startswith("skill not in library")]
    for n, m in probs:
        print(f"✗ {n}: {m}")
    return 1 if probs else 0


if __name__ == "__main__":
    sys.exit(main())
