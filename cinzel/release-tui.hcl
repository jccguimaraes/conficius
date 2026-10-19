workflow "release_tui" {
  filename = "release-tui"

  name = "Release TUI"

  on "push" {
    tags = ["tui-v*"]
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
    job.verify_2,
    job.publish_2,
  ]
}

job "verify_2" {
  id = "verify"

  name = "Verify TUI release"

  permissions {
    contents = "read"
  }

  runs_on {
    runners = "ubuntu-latest"
  }

  timeout_minutes = 15

  steps = [
    step.check_out_repository_3,
    step.validate_release_tag_2,
    step.set_up_go_3,
    step.test_3,
    step.build_snapshot_2,
  ]
}

step "check_out_repository_3" {
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

step "validate_release_tag_2" {
  ignore_id = true

  name = "Validate release tag"

  run = <<-EOF
if ! printf '%s\n' "$TAG" | grep -Eq '^tui-v(20[0-9]{2})\.(0[1-9]|1[0-2])\.([1-9][0-9]*)$'; then
  echo "invalid TUI release tag: $TAG" >&2
  exit 1
fi
git fetch origin main --no-tags
git merge-base --is-ancestor "$GITHUB_SHA" origin/main
echo "CONFICIUS_VERSION=$${TAG#tui-v}" >> "$GITHUB_ENV"
EOF

  env {
    name  = "TAG"
    value = "$${{ github.ref_name }}"
  }
}

step "set_up_go_3" {
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

step "test_3" {
  ignore_id = true

  name = "Test"

  run = "go test -race ./..."
}

step "build_snapshot_2" {
  ignore_id = true

  name = "Build snapshot"

  uses {
    action  = "goreleaser/goreleaser-action"
    version = "a4f02ab5355a589de60d917a659ce617ed19b3de" # v7.3.0
  }

  with {
    name  = "args"
    value = "build --snapshot --clean --config .goreleaser-tui.yaml"
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

job "publish_2" {
  id = "publish"

  if = "github.event_name == 'workflow_dispatch' && github.ref_type == 'tag' && startsWith(github.ref_name, 'tui-v') && inputs.confirm == 'publish'"

  name = "Publish TUI release"

  depends_on = [
    job.verify_2,
  ]

  permissions {
    contents = "write"
  }

  runs_on {
    runners = "ubuntu-latest"
  }

  timeout_minutes = 15

  steps = [
    step.check_out_repository_3,
    step.set_release_version_2,
    step.set_up_go_3,
    step.publish_with_goreleaser_2,
  ]
}

step "set_release_version_2" {
  ignore_id = true

  name = "Set release version"

  run = "echo \"CONFICIUS_VERSION=$${TAG#tui-v}\" >> \"$GITHUB_ENV\""

  env {
    name  = "TAG"
    value = "$${{ github.ref_name }}"
  }
}

step "publish_with_goreleaser_2" {
  ignore_id = true

  name = "Publish with GoReleaser"

  uses {
    action  = "goreleaser/goreleaser-action"
    version = "a4f02ab5355a589de60d917a659ce617ed19b3de" # v7.3.0
  }

  with {
    name  = "args"
    value = "release --clean --skip=validate --config .goreleaser-tui.yaml"
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

  env {
    name  = "HOMEBREW_TAP_TOKEN"
    value = "$${{ secrets.HOMEBREW_TAP_TOKEN }}"
  }
}
