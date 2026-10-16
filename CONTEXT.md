# Conficius

Conficius describes and inspects dotfiles, package installations, and package update intent across a machine and its projects.

## Language

**Profile**:
A named group of projects and enabled package managers. Native project manifests define dependency versions.
_Avoid_: Preset, environment

**Global scope**:
Package installations owned by the current user or machine rather than one project.
_Avoid_: System scope

**Project scope**:
Package installations and manifests associated with one canonical project path.
_Avoid_: Local scope

**Resolved profile**:
The profile selected from central package configuration by an explicit CLI or TUI choice, environment selection, configured default, or built-in default, then validated.
_Avoid_: Merged config

**Update plan**:
An ordered, immutable description of package changes that Conficius can preview and re-check before execution.
_Avoid_: Script, batch

**Manager adapter**:
The boundary that owns one package manager's capabilities, package identity, command invocation, and output parsing.
_Avoid_: Plugin, driver
