# C / C++ — What Good Looks Like

Apply when planning, implementing, or reviewing code detected by `.c`, `.cc`, `.cpp`, `.h`, `.hpp`; CMake, Makefile. Dispatched via `shared/language-verification.md`.

## Sanitizers

| Tool | When | What it catches |
|------|------|-----------------|
| ASAN (AddressSanitizer) | Always | Buffer overflows, use-after-free, double-free, stack overflows |
| UBSAN (UndefinedBehaviorSanitizer) | Always | Signed integer overflow, null pointer dereference, misaligned access |
| TSAN (ThreadSanitizer) | Threads, atomics, shared mutable state | Data races, lock order violations |
| MSAN (MemorySanitizer) | When processing external/untrusted input | Use of uninitialized memory |

**Minimum bar:** ASAN + UBSAN on every phase. Add TSAN when concurrency is involved.

**Build integration:** Typically `-fsanitize=address,undefined` (Clang/GCC). TSAN and ASAN cannot run simultaneously — use separate build configs.

## Const Correctness

- **Parameters:** primitives/enums by `const` value (`const int32_t size`), complex types by `const&` (`const std::string& name`)
- **Methods:** mark non-mutating methods `const`
- **Local variables:** use `const` for anything that doesn't change after initialization
- **Calculations:** assign intermediate results to named `const` variables rather than inlining expressions

## Ownership & Memory

- **Smart pointers over raw:** `std::unique_ptr` for exclusive ownership, `std::shared_ptr` only when genuinely shared
- **`std::span<T>` for non-owning sequences:** prefer over `const std::vector<T>&` for function parameters — decouples the interface from the container
- **No naked `new`/`delete`:** use `std::make_unique` / `std::make_shared`
- **Move semantics:** use `std::move` when transferring ownership, don't copy when a move suffices

## Return Values & Error Handling

- **`[[nodiscard]]` on all functions returning values** — forces callers to handle the result
- **Result/Expected pattern over exceptions:** if the project uses `Result<T>`, `std::expected`, or similar — use it consistently. Don't mix with exceptions
- **No silent failure:** every failable operation must return an error path the caller can observe

## Thread Safety

- **`std::scoped_lock` over `std::lock_guard`** — handles multiple mutexes, less error-prone
- **Clang thread safety annotations** (`GUARDED_BY`, `REQUIRES`, `EXCLUDES`) on all classes with mutexes — turns data races into compile-time errors
- **TSAN annotations** (`ANNOTATE_HAPPENS_BEFORE/AFTER`) for custom synchronization that the compiler can't see (lock-free structures, condition variables with unusual patterns)
- **ASAN memory annotations** (`POISON/UNPOISON_MEMORY_REGION`) for containers that reuse buffer memory

## Modern C++ Idioms

- **Brace initialization for members:** `int mCount{ 0 };` — prevents narrowing conversions
- **`explicit` constructors** on single-argument and simple constructors — prevents implicit conversions
- **`std::optional<T>`** for nullable/absent state — not raw pointers or sentinel values
- **`std::string_view`** for read-only string parameters — avoids copies, works with both `std::string` and string literals
- **If-init statements:** `if (const auto it = map.find(k); it != map.end())` — limits variable scope
- **Mark final implementations `final`** — enables devirtualization
- **Forward declarations** in headers to minimize include dependencies

## What to Look For in Review

When reviewing C++ code against a plan, check:
- Are sanitizer builds defined and passing? (not just debug/release)
- Are returned values `[[nodiscard]]`? Are callers handling them?
- Are parameters const-correct? Are methods marked `const` where possible?
- Is ownership clear? (unique_ptr vs shared_ptr vs raw pointer — each should be deliberate)
- Are mutexes annotated with thread safety attributes?
- Are spans used instead of const vector references at API boundaries?
