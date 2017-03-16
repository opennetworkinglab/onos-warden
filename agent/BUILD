load("@io_bazel_rules_go//go:def.bzl", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "common.go",
        "grpc.go",
        "key.go",
        "ssh.go",
        "words.go",
    ],
    visibility = ["//visibility:public"],
    deps = [
        "//util:go_default_library",
        "//warden:go_default_library",
        "@org_golang_google_grpc//:go_default_library",
        "@org_golang_google_grpc//credentials:go_default_library",
        "@org_golang_google_grpc//grpclog:go_default_library",
        "@org_golang_x_crypto//ssh:go_default_library",
    ],
)

go_test(
    name = "go_default_test",
    srcs = ["key_test.go"],
    library = ":go_default_library",
)
