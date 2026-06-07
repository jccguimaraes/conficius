# Conficius

An atempt to be a modern alternative to the fantastic [GNU Stow](https://www.gnu.org/software/stow/) project.

<p align="center">
  <img src="./docs/assets/conficius-gopher.png" alt="Conficius Gopher" width="300">
</p>

> Inspired by the Go [gopher](https://go.dev/doc/gopher) mascot created by Renee French.

> Supporting only `Linux` and `macOS` for now.

## Why?

Conficius tries to go further and inforce best practices for configuration files depending on the OS, either by checking that `$HOME` environment variable is set and depending on the shell you're using, make sure environment variables are set in their right place so tool can point to the right config folder.

As an example:

For the `zsh` shell, it checks and suggests setting the environment variables  `$XDG_CONFIG_HOME` and `$ZDOTDIR` on `$HOME/.zprofile`.

```sh
# $HOME/.zprofile
XDG_CONFIG_HOME="${HOME}/Library/Application Support" # (on macOS)
XDG_CONFIG_HOME="${HOME}/.config" # (on linux)
ZDOTDIR="${XDG_CONFIG_HOME}/zsh"
```

This will tell zsh where the `.zshrc` file is located, instead of the historical `~/.zshrc`.

## Contribution

First things first run `mise run setup`

- Sets the local git configuration `core.hooksPath`.

## Principal

- Detects Operation System (OS)
- Detects Arch (needed?)
- Detects shell
- Detects terminal (needed?)

- Detects XDG env var
- Validates it matches OS standard
  - Linux: ~/.config
  - macOS: ~/Library/Application Support
  - Windows: %AppData%
