def _use_repos_impl(repository_ctx):
    repository_ctx.file("WORKSPACE.bazel")
    repository_ctx.file("BUILD.bazel", """exports_files(["repositories.json"])""")
    repository_ctx.file("repositories.json", json.encode(repository_ctx.attr.root_repos))

use_repos = repository_rule(
    implementation = _use_repos_impl,
    attrs = {
        "root_repos": attr.string_list(),
    },
)
