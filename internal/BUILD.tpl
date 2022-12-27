load("@rules_go//go:def.bzl", "go_binary")

go_binary(
    name = "update",
    args = %{args},
    data = %{data},
    visibility = ["@auto_use_repo//:__pkg__"],
    embed = ["@auto_use_repo//cmd:main"],
)