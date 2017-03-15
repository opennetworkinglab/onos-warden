load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["dummy.go"],
    visibility = ["//visibility:private"],
    deps = [
        "//agent:go_default_library",
        "//warden:go_default_library",
    ],
)

go_binary(
    name = "dummy",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)
