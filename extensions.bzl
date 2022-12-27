def _update_attrs_impl(repository_ctx):
    repository_ctx.file("BUILD.bazel")
    repository_ctx.file("WORKSPACE.bazel")

    args = repr([
        "{}=$(rlocationpath {})".format(name, file)
        for name, file in repository_ctx.attr.extensions.items()
    ])
    data = repr(repository_ctx.attr.extensions.values())
    repository_ctx.file(
        "update_attrs.bzl",
        "\n".join([
            "UPDATE_ARGS = " + args,
            "UPDATE_DATA = " + data,
        ]),
    )

_update_attrs = repository_rule(
    implementation = _update_attrs_impl,
    attrs = {
        "extensions": attr.string_dict(),
    },
)

_register_tag = tag_class(
    attrs = {
        "extension_bzl_file": attr.label(mandatory = True),
        "extension_name": attr.string(mandatory = True),
        "repositories_json_file": attr.label(mandatory = True),
    },
)

def _auto_use_repo_impl(module_ctx):
    extensions = {}

    for module in module_ctx.modules:
        for tag in module.tags.register:
            id = "@{}//{}:{}%{}".format(
                module.name,
                tag.extension_bzl_file.package,
                tag.extension_bzl_file.name,
                tag.extension_name,
            )
            if id in extensions:
                fail("Duplicate extension registration: {}".format(id))
            extensions[id] = str(tag.repositories_json_file)

    _update_attrs(
        name = "internal_only_update_attrs",
        extensions = extensions,
    )

auto_use_repo = module_extension(
    implementation = _auto_use_repo_impl,
    tag_classes = {
        "register": _register_tag,
    },
)
