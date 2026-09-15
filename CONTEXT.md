# Conficius

Conficius describes and inspects dotfiles, package installations, and package update intent across a machine and its projects.

## Language

**Profile**:
A named policy that selects package managers, projects, packages, desired versions, and inspection behavior.
_Avoid_: Preset, environment

**Global scope**:
Package installations owned by the current user or machine rather than one project.
_Avoid_: System scope

**Project scope**:
Package installations and manifests associated with one canonical project path.
_Avoid_: Local scope

**Resolved profile**:
The effective profile after defaults, global configuration, project configuration, environment selection, and CLI selection have been combined and validated.
_Avoid_: Merged config

**Desired version**:
The exact package version stated by a profile. It may be reported as unsupported when a manager cannot provide that exact version.
_Avoid_: Fixed version, version range

**Update plan**:
An ordered, immutable description of package changes that Conficius can preview and re-check before execution.
_Avoid_: Script, batch

**Manager adapter**:
The boundary that owns one package manager's capabilities, package identity, command invocation, and output parsing.
_Avoid_: Plugin, driver
