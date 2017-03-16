load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test")

go_library(
    name = "go_default_library",
    srcs = [
        "ec2.go",
        "ec2-provision.go",
        "ec2-request.go",
    ],
    visibility = ["//visibility:private"],
    deps = [
        "//agent:go_default_library",
        "//warden:go_default_library",
        "@com_github_aws_aws_sdk_go//aws:go_default_library",
        "@com_github_aws_aws_sdk_go//aws/session:go_default_library",
        "@com_github_aws_aws_sdk_go//service/ec2:go_default_library",
        "@org_golang_x_crypto//ssh:go_default_library",
    ],
)

go_binary(
    name = "ec2",
    library = ":go_default_library",
    visibility = ["//visibility:public"],
)

go_test(
    name = "go_default_test",
    srcs = ["ec2_test.go"],
    library = ":go_default_library",
    deps = ["//warden:go_default_library"],
)
