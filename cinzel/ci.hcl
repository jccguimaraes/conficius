workflow "ci" {
  filename = "ci"

  concurrency {
    cancel_in_progress = true
    group              = "ci-$${{ github.workflow }}-$${{ github.ref }}"
  }

  name = "CI"

  on "pull_request" {
  }

  on "push" {
    branches = ["main"]
  }

  permissions {
    contents = "read"
  }

  jobs = [
    job.test,
    job.lint,
  ]
}

job "test" {
  name = "Test ($${{ matrix.os }})"

  permissions {
    contents = "read"
  }

  runs_on {
    runners = "$${{ matrix.os }}"
  }

  strategy {
    fail_fast = false

    matrix {
      variable {
        name  = "os"
        value = variable.list_os
      }
    }
  }

  timeout_minutes = 15

  steps = [
    step.check_out_repository,
    step.set_up_go,
    step.test,
    step.vet,
    step.build,
    step.smoke_test,
  ]
}

step "check_out_repository" {
  ignore_id = true

  name = "Check out repository"

  uses {
    action  = "actions/checkout"
    version = "3d3c42e5aac5ba805825da76410c181273ba90b1" # v7.0.1
  }

  with {
    name  = "persist-credentials"
    value = false
  }
}

step "set_up_go" {
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

step "test" {
  ignore_id = true

  name = "Test"

  run = "go test -race ./..."
}

step "vet" {
  ignore_id = true

  name = "Vet"

  run = "go vet ./..."
}

step "build" {
  ignore_id = true

  name = "Build"

  run = <<-EOF
go build -o ./bin/conficius ./cmd/cli
go build -o ./bin/conficius-tui ./cmd/tui
EOF
}

step "smoke_test" {
  ignore_id = true

  name = "Smoke test"

  run = <<-EOF
./bin/conficius --help >/dev/null
./bin/conficius packages pms --json >/dev/null
EOF
}

job "lint" {
  name = "Lint"

  permissions {
    contents = "read"
  }

  runs_on {
    runners = "ubuntu-latest"
  }

  timeout_minutes = 15

  steps = [
    step.check_out_repository,
    step.install_tools,
    step.lint_go,
    step.check_goreleaser,
    step.check_hooks,
  ]
}

step "install_tools" {
  ignore_id = true

  name = "Install tools"

  uses {
    action  = "jdx/mise-action"
    version = "08ae2a091c038e1e05ec6f5115dc5c69d5a2a6b8" # v5.1.2
  }

  with {
    name  = "install_args"
    value = "go golangci-lint goreleaser cinzel hk pkl actionlint ghalint git-cliff"
  }
}

step "lint_go" {
  ignore_id = true

  name = "Lint Go"

  run = "golangci-lint run"
}

step "check_goreleaser" {
  ignore_id = true

  name = "Check GoReleaser"

  run = <<-EOF
goreleaser check
goreleaser check --config .goreleaser-tui.yaml
EOF
}

variable "list_os" {
  value = ["ubuntu-latest", "macos-latest"]
}

step "check_hooks" {
  ignore_id = true
  name      = "Check hooks and generated workflows"
  run       = "mise run check"
}
