<p align="center"><img src="docs/logo/cellar.svg" width="96" alt=""></p>

# cellar

A Wine prefix manager for Linux, in the spirit of Bottles, where each prefix
is described by one declarative file, `cellar.toml`. The GUI, the command line
and anything scripted (an AI agent included) all edit that same file, and
cellar reconciles the prefix to match it.

The same engine doubles as the launcher inside Wine apps packaged for
FlatPark: a package ships a `cellar.toml` and the
vendor's installer, and cellar builds and maintains each user's prefix from it.

## Features

- **Declarative prefixes**: runner, locale, fonts, Windows version, virtual
  desktop, DPI, drives, winetricks verbs and apps, all in `cellar.toml`.
  `plan` shows what would change, `apply` makes it so, and `run` applies first
  whenever the file changed. Registry values cellar did not set are left alone.
- **Runners**: the system Wine, or community builds downloaded on demand
  (Kron4ek vanilla / staging / staging-tkg / proton, Bottles' vaniglia).
  WoW64 builds run 32-bit programs with no 32-bit host libraries.
- **Fonts**: core and CJK font sets copied into the prefix and wired up with
  font links and substitutes, so CJK text renders without manual registry work.
- **Dependencies**: every winetricks verb, searchable, with the common ones
  described.
- **Apps**: finds installed programs from Start menu shortcuts and `drive_c`,
  extracts their icons and adds desktop launchers.
- **Logs**: every run is recorded (the last 20 per prefix), console output in
  the prefix's code page decoded for reading.
- **Bottles import**: brings over existing bottles, their DLL overrides and
  settings included.

## Install

[Releases](https://github.com/jing2uo/cellar/releases) carry two x86_64
binaries, with their `SHA256SUMS`:

- `cellar-x86_64`: the GUI (Wails v3, GTK 4 / WebKitGTK 6) plus the full CLI
- `cellar-cli-x86_64`: a static, GUI-less build of the CLI

Builds of every commit are attached to the
[CI runs](https://github.com/jing2uo/cellar/actions/workflows/build.yml).

A Flatpak (`org.flatpark.Cellar`, on the `org.winehq.Wine` base) is planned for
FlatPark.

## Usage

Run `cellar` with no arguments for the GUI, or use the CLI:

```sh
cellar new office --locale zh_CN        # create a prefix and apply it
cellar run office --exe ~/Downloads/setup.exe
cellar app discover office              # programs installed but not added yet
cellar app add office 'C:\Program Files\App\app.exe'
cellar run office/app

cellar config office winver=win10 dpi=144
cellar deps office add vcrun2022
cellar edit office                      # edit cellar.toml, then apply
```

`cellar help` lists every command. All of them accept `--json`. Data lives in
`$CELLAR_HOME`, by default `$XDG_DATA_HOME/cellar`:

```
prefixes/<name>/cellar.toml   the declaration
prefixes/<name>/pfx/          the WINEPREFIX
prefixes/<name>/runs/         a log per run
runners/                      downloaded Wine builds
```

A minimal `cellar.toml`:

```toml
locale = "zh_CN"
font_sets = ["core", "cjk"]

[[apps]]
id = "notepadpp"
name = "Notepad++"
exe = 'C:\Program Files\Notepad++\notepad++.exe'
```

`cellar inspect <prefix>` prints the effective configuration, with the built-in
defaults merged in.

## Packaging Wine apps

[packaging/examples](packaging/examples) holds FlatPark package examples
(7-Zip, foobar2000, Notepad++). Each ships the vendor's installer unmodified as
extra-data, and its `cellar.toml` says how to install it:

```toml
[install]
source = "7zip-setup.exe"
method = "run"      # run the installer in the user's prefix on first launch
args = ["/S"]
```

Inside a package, `cellar` with no arguments starts the default app, and the
user's settings are changed the same way:

```sh
flatpak run --command=cellar org._7_zip.SevenZip config locale=ja_JP
```

## Building

Requires Go (see `go.mod`).

```sh
make cli              # dist/cellar-cli: static, no cgo
make image && make gui  # dist/cellar: built in a podman container with the
                        # GTK 4 / WebKitGTK 6 dev packages
make test
make flatpak          # build and install the local Flatpak
```

Versions come from the `vX.Y.Z` tags (`scripts/version.sh`): a build after a
release reports e.g. `0.1.0-3-gabc1234`. To cut a release, run the
[release workflow](https://github.com/jing2uo/cellar/actions/workflows/release.yml)
and pick patch, minor or major; it tags the next version, builds and publishes
it. Pushing a `vX.Y.Z` tag does the same.

## License

[GPL-3.0-or-later](LICENSE)
