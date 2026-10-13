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
    version = "983d7736d9b0ae728b81ab479565c72886d7745b" # v5
  }
}

step "upload_site" {
  ignore_id = true
  name      = "Upload site"

  uses {
    action  = "actions/upload-pages-artifact"
    version = "7b1f4a764d45c48632c6b24a0339c27f5614fb0b" # v4
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
    version = "d6db90164ac5ed86f2b6aed7e0febac5b3c0c03e" # v4
  }
}
