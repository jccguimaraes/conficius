workflow "release" {
  filename = "release"
  name     = "Release"

  on "workflow_dispatch" {
    input "product" {
      description = "Product to publish"
      required    = true
      type        = "choice"
      options     = ["cli", "tui"]
    }

    input "version" {
      description = "Optional version override; leave blank to calculate"
      required    = false
      type        = "string"
    }


  }

  concurrency {
    group              = "release-publishing"
    cancel_in_progress = false
  }

  permissions {
    contents = "read"
  }

  jobs = [job.verify, job.publish]
}

job "verify" {
  permissions { contents = "read" }

  if = "github.ref == 'refs/heads/main'"

  runs_on { runners = "ubuntu-latest" }

  timeout_minutes = 20

  output {
    name  = "tag"
    value = "$${{ steps.validate.outputs.tag }}"
  }

  output {
    name  = "config"
    value = "$${{ steps.validate.outputs.config }}"
  }

  output {
    name = "version"
    value = "$${{ steps.validate.outputs.version }}"
  }

  steps = [step.checkout, step.check_sha, step.tools, step.validate, step.check, step.snapshot]
}

step "checkout" {
  ignore_id = true

  uses {
    action  = "actions/checkout"
    version = "3d3c42e5aac5ba805825da76410c181273ba90b1"
  }

  with {
    name  = "ref"
    value = "$${{ github.sha }}"
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

step "tools" {
  ignore_id = true

  uses {
    action  = "jdx/mise-action"
    version = "08ae2a091c038e1e05ec6f5115dc5c69d5a2a6b8"
  }

  with {
    name  = "install_args"
    value = "go goreleaser git-cliff cinzel hk pkl actionlint ghalint gh"
  }
}

step "validate" {
  run = <<-EOF
VERSION="$(VERSION="$VERSION" mise run --quiet next-version)"
case "$PRODUCT" in
  cli)
    [[ "$VERSION" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.(0|[1-9][0-9]*))?$ ]]
    config=.goreleaser.yaml
    ;;
  tui)
    [[ "$VERSION" =~ ^20[0-9]{2}\.(0[1-9]|1[0-2])\.[1-9][0-9]*$ ]]
    config=.goreleaser-tui.yaml
    ;;
  *) exit 1 ;;
esac
tag="$PRODUCT-v$VERSION"
if git show-ref --verify --quiet "refs/tags/$tag"; then
  printf '%s\n' "Tag already exists: $tag; inspect partial releases before recovery" >&2
  exit 1
fi
printf 'tag=%s\nconfig=%s\nversion=%s\n' "$tag" "$config" "$VERSION" >> "$GITHUB_OUTPUT"
EOF

  env {
    name  = "PRODUCT"
    value = "$${{ inputs.product }}"
  }

  env {
    name  = "VERSION"
    value = "$${{ inputs.version }}"
  }

  env {
    name  = "SHA"
    value = "$${{ github.sha }}"
  }
}

step "check" {
  ignore_id = true

  run = "mise run test-ci && mise run check && mise run release-check"
}

step "notes" {
  ignore_id = true

  run = "mise run release-notes > /tmp/release-notes.md"

  env {
    name  = "PRODUCT"
    value = "$${{ inputs.product }}"
  }

  env {
    name  = "VERSION"
    value = "$${{ needs.verify.outputs.version }}"
  }

  env {
    name  = "SHA"
    value = "$${{ github.sha }}"
  }
}

step "snapshot" {
  ignore_id = true

  run = "goreleaser release --snapshot --clean --config \"$CONFIG\""

  env {
    name  = "CONFIG"
    value = "$${{ steps.validate.outputs.config }}"
  }

  env {
    name  = "CONFICIUS_VERSION"
    value = "$${{ steps.validate.outputs.version }}"
  }
}

job "publish" {
  depends_on = [job.verify]

  runs_on { runners = "ubuntu-latest" }

  timeout_minutes = 20

  environment { name = "release" }

  permissions { contents = "write" }

  steps = [step.checkout, step.check_sha, step.tools, step.notes, step.create_tag, step.package]
}

step "create_tag" {
  ignore_id = true

  run = "gh api --method POST \"repos/$GITHUB_REPOSITORY/git/refs\" -f ref=\"refs/tags/$TAG\" -f sha=\"$SHA\" && git fetch origin \"refs/tags/$TAG:refs/tags/$TAG\""

  env {
    name  = "GH_TOKEN"
    value = "$${{ github.token }}"
  }

  env {
    name  = "TAG"
    value = "$${{ needs.verify.outputs.tag }}"
  }

  env {
    name  = "SHA"
    value = "$${{ github.sha }}"
  }
}

step "package" {
  ignore_id = true

  run = <<-EOF
case "$CONFICIUS_VERSION" in
  *-*) sed 's/prerelease: "false"/prerelease: "true"/' "$CONFIG" > /tmp/goreleaser.yaml; CONFIG=/tmp/goreleaser.yaml ;;
esac
goreleaser release --clean --skip=validate --config "$CONFIG" --release-notes /tmp/release-notes.md
EOF

  env {
    name  = "CONFIG"
    value = "$${{ needs.verify.outputs.config }}"
  }

  env {
    name  = "CONFICIUS_VERSION"
    value = "$${{ needs.verify.outputs.version }}"
  }

  env {
    name  = "GORELEASER_CURRENT_TAG"
    value = "$${{ needs.verify.outputs.tag }}"
  }

  env {
    name  = "GITHUB_TOKEN"
    value = "$${{ github.token }}"
  }

  env {
    name  = "HOMEBREW_TAP_TOKEN"
    value = "$${{ secrets.HOMEBREW_TAP_TOKEN }}"
  }
}

step "check_sha" {
  ignore_id = true

  run = <<-EOF
[[ "$SHA" =~ ^[0-9a-fA-F]{40}$ ]]
[ "$(git rev-parse HEAD)" = "$(git rev-parse "$SHA^{commit}")" ]
git merge-base --is-ancestor "$SHA" origin/main
EOF

  env {
    name  = "SHA"
    value = "$${{ github.sha }}"
  }
}
