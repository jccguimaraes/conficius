workflow "pages" {
  filename = "pages"
  name     = "GitHub Pages"

  on "push" {
    branches = ["main"]
    paths    = ["docs/**", "cinzel/pages.hcl", ".github/workflows/pages.yml"]
  }

  on "workflow_dispatch" {
  }

  permissions {
    contents = "read"
  }

  concurrency {
    group              = "pages"
    cancel_in_progress = false
  }

  jobs = [job.deploy_site]
}

job "deploy_site" {
  name = "Deploy site"

  runs_on {
    runners = "ubuntu-latest"
  }

  permissions {
    contents = "read"
    pages    = "write"
    id_token = "write"
  }

  environment {
    name = "github-pages"
    url  = "$${{ steps.deploy_pages.outputs.page_url }}"
  }

  if              = "$${{ github.ref == 'refs/heads/main' }}"
  timeout_minutes = 10
  steps           = [step.pages_checkout, step.configure_pages, step.upload_site, step.deploy_pages]
}

step "pages_checkout" {
  ignore_id = true
  name      = "Check out repository"

  uses {
    action  = "actions/checkout"
    version = "3d3c42e5aac5ba805825da76410c181273ba90b1"
  }

  with {
    name  = "persist-credentials"
    value = false
  }
}

step "configure_pages" {
  ignore_id = true
  name      = "Configure Pages"

  uses {
    action  = "actions/configure-pages"
    version = "45bfe0192ca1faeb007ade9deae92b16b8254a0d" # v6.0.0
  }
}

step "upload_site" {
  ignore_id = true
  name      = "Upload site"

  uses {
    action  = "actions/upload-pages-artifact"
    version = "fc324d3547104276b827a68afc52ff2a11cc49c9" # v5.0.0
  }

  with {
    name  = "path"
    value = "docs"
  }
}

step "deploy_pages" {
  name = "Deploy to GitHub Pages"

  uses {
    action  = "actions/deploy-pages"
    version = "368f82528645a54fb793d4d04e342629a3f51346" # v5.0.1
  }
}
