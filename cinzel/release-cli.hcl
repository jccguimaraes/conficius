workflow "release_cli" {
  filename = "release-cli"

  name = "Release CLI"

  on "push" {
    tags = ["cli-v*"]
  }

  on "workflow_dispatch" {
    input "confirm" {
      default     = "verify"
      description = "Type \"publish\" to release selected tag"
      required    = false
      type        = "string"
    }
  }

  permissions {
    contents = "read"
  }

  jobs = [
    job.verify,
    job.publish,
  ]
}

job "verify" {
  name = "Verify CLI release"

  permissions {
    contents = "read"
  }

  runs_on {
    runners = "ubuntu-latest"
  }

  timeout_minutes = 15

  steps = [
    step.check_out_repository_2,
    step.validate_release_tag,
    step.set_up_go_2,
    step.test_2,
    step.build_snapshot,
  ]
}

step "check_out_repository_2" {
  ignore_id = true

  name = "Check out repository"

  uses {
    action  = "actions/checkout"
    version = "3d3c42e5aac5ba805825da76410c181273ba90b1" # v7.0.1
  }

  with {
    name  = "fetch-depth"
    value = 0
  }

  with {
    name  = "persist-credentials"
    value = false
  }
}

step "validate_release_tag" {
  ignore_id = true

  name = "Validate release tag"

  run = <<-EOF
if ! printf '%s\n' "$TAG" | grep -Eq '^cli-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'; then
  echo "invalid CLI release tag: $TAG" >&2
  exit 1
fi
git fetch origin main --no-tags
git merge-base --is-ancestor "$GITHUB_SHA" origin/main
version=$${TAG#cli-v}
grep -Fqx "## [$version]" CHANGELOG.md
echo "CONFICIUS_VERSION=$version" >> "$GITHUB_ENV"
EOF

  env {
    name  = "TAG"
    value = "$${{ github.ref_name }}"
  }
}

step "set_up_go_2" {
  ignore_id = true

  name = "Set up Go"

  uses {
    action  = "actions/setup-go"
    version = "b7ad1dad31e06c5925ef5d2fc7ad053ef454303e" # v7.0.0
  }

  with {
    name  = "cache-dependency-path"
    value = "go.sum"
  }

  with {
    name  = "go-version-file"
    value = "go.mod"
  }
}

step "test_2" {
  ignore_id = true

  name = "Test"

  run = "go test -race ./..."
}

step "build_snapshot" {
  ignore_id = true

  name = "Build snapshot"

  uses {
    action  = "goreleaser/goreleaser-action"
    version = "f06c13b6b1a9625abc9e6e439d9c05a8f2190e94" # v7.2.3
  }

  with {
    name  = "args"
    value = "build --snapshot --clean"
  }

  with {
    name  = "distribution"
    value = "goreleaser"
  }

  with {
    name  = "version"
    value = "v2.18.1"
  }
}

job "publish" {
  if = "github.event_name == 'workflow_dispatch' && github.ref_type == 'tag' && startsWith(github.ref_name, 'cli-v') && inputs.confirm == 'publish'"

  name = "Publish CLI release"

  depends_on = [
    job.verify,
  ]

  permissions {
    contents = "write"
  }

  runs_on {
    runners = "ubuntu-latest"
  }

  timeout_minutes = 15

  steps = [
    step.check_out_repository_2,
    step.set_release_version,
    step.set_up_go_2,
    step.publish_with_goreleaser,
  ]
}

step "set_release_version" {
  ignore_id = true

  name = "Set release version"

  run = "echo \"CONFICIUS_VERSION=$${TAG#cli-v}\" >> \"$GITHUB_ENV\""

  env {
    name  = "TAG"
    value = "$${{ github.ref_name }}"
  }
}

step "publish_with_goreleaser" {
  ignore_id = true

  name = "Publish with GoReleaser"

  uses {
    action  = "goreleaser/goreleaser-action"
    version = "f06c13b6b1a9625abc9e6e439d9c05a8f2190e94" # v7.2.3
  }

  with {
    name  = "args"
    value = "release --clean --skip=validate"
  }

  with {
    name  = "distribution"
    value = "goreleaser"
  }

  with {
    name  = "version"
    value = "v2.18.1"
  }

  env {
    name  = "GORELEASER_CURRENT_TAG"
    value = "$${{ github.ref_name }}"
  }

  env {
    name  = "GITHUB_TOKEN"
    value = "$${{ secrets.GITHUB_TOKEN }}"
  }
}
