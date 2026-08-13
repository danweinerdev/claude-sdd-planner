# TypeScript / JavaScript — Structural Verification

Apply when planning, implementing, or reviewing code detected by `.ts`, `.tsx`, `.js`, `.jsx`; `tsconfig.json`, `package.json`. Dispatched via `shared/language-verification.md`.

## Tools

| Tool | When | What it catches |
|------|------|-----------------|
| `tsc --noEmit` | TypeScript projects | Type errors, missing properties, incorrect generics |
| Linter (eslint / biome) | If configured in project | Unused variables, unreachable code, common bugs |

## Minimum Bar

Type-check TypeScript projects. Run the linter if configured.
