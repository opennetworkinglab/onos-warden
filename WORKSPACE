git_repository(
    name = "io_bazel_rules_go",
    remote = "https://github.com/bazelbuild/rules_go.git",
    tag = "0.4.1",
)
load("@io_bazel_rules_go//go:def.bzl", "go_repositories", "new_go_repository")
go_repositories()

load("@io_bazel_rules_go//proto:go_proto_library.bzl", "go_proto_repositories")
go_proto_repositories()


new_go_repository(
    name = "com_github_golang_protobuf",
    importpath = "github.com/golang/protobuf",
    tag = "c9c7427a2a70d2eb3bafa0ab2dc163e45f143317",
)

new_go_repository(
    name = "org_golang_google_grpc",
    importpath = "google.golang.org/grpc",
    commit = "0713829b980f4ddd276689a36235c5fcc82a21bf",
)

new_go_repository(
    name = "org_golang_x_net",
    importpath = "golang.org/x/net",
    tag = "a6577fac2d73be281a500b310739095313165611",
)

new_go_repository(
    name = "org_golang_x_crypto",
    importpath = "golang.org/x/crypto",
    tag = "728b753d0135da6801d45a38e6f43ff55779c5c2",
)