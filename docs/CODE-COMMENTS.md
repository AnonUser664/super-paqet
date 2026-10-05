# Source comment coverage

Comment pass based on committed `adec55a`; it changes no transport/runtime
implementation, dependency version, CI command or build target. It also leaves
recorded deployed config/unit snapshots untouched. New package doc.go files
contain only package documentation/declarations.

Coverage includes all application/transport Go files, both local library forks,
tests and benchmark functions, production struct/interface members, top-level
constants/globals, Python harness modules/functions, module/build definitions
and CI workflow purposes. Existing upstream licenses and detailed algorithm
comments are retained. Comments explain contracts, units, buffer/socket
ownership, locking, failure handling and the reasons for preserved wire choices.
Simple statements remain readable code; comments concentrate on purpose and
invariants instead of mechanically describing each assignment.

The pass covers 853 named Go functions and 132 Go type declarations
with declaration documentation. An AST coverage audit found no uncovered
function/type declarations or production struct/interface fields. It added
field explanations for 474 previously undocumented members and purpose comments
for 580 previously undocumented functions. Test fixture behavior is documented
at type/function scope; existing assertion-specific comments remain.

Go syntax trees with source positions/comments excluded match the baseline for
all 131 existing Go source files. Build/compiler directives also match. Python
ASTs are identical before/after. Removing comments from Makefile, module files
and workflow files produces their original content. These checks protect
against accidental changes hidden by formatting or inserted comments.

The six pre-existing dirty timeout/wire experiment files are documented
separately; their original executable syntax is preserved. Their implementation
changes remain outside the comments-only checkpoint and are not qualified by
this annotation pass. Source architecture/version boundaries remain in
[ARCHITECTURE.md](ARCHITECTURE.md) and [STATUS.md](STATUS.md).

Validation collected after this pass: root race suite and vet passed; full KCP
tests/vet passed (122.115 seconds for the tests); full smux race suite/vet passed
(517.698 seconds for the race tests). The main-workspace AST/directive check
also confirms all six pending experiments retain their original executable
syntax after restoration. No deployment or tuning occurred during this pass.
