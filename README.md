# Kantra

Kantra is an experimental CLI that unifies analysis and transformation capabilities of Konveyor.

## Installation
The easiest way to install Kantra is to get it via the container image. To download latest container image, run:

### Linux

```sh
podman pull quay.io/konveyor/kantra:latest && podman run --name kantra-download quay.io/konveyor/kantra:latest 1> /dev/null 2> /dev/null && podman cp kantra-download:/usr/local/bin/kantra . && podman rm kantra-download
```

### MacOS

**Note:** There is a known [issue](https://github.com/containers/podman/issues/16106)
with limited number of open files in mounted volumes on MacOS, which may affect kantra performance.

Prior to starting your podman machine, run:

```sh
ulimit -n unlimited
```

 - This must be run after each podman machine reboot.

In order to correctly mount volumes, your podman machine must contain options:

```sh
podman machine init <vm_name> -v $HOME:$HOME -v /private/tmp:/private/tmp -v /var/folders/:/var/folders/
```

Increase podman resources:

```sh
podman machine set <vm_name> --cpus 4 --memory 4096
```


Ensure that we use the connection to the VM `<vm_name>` we created earlier by default:

```sh
podman system connection default <vm_name>
```

```sh
podman pull quay.io/konveyor/kantra:latest && podman run --name kantra-download quay.io/konveyor/kantra:latest 1> /dev/null 2> /dev/null && podman cp kantra-download:/usr/local/bin/darwin-kantra kantra && podman rm kantra-download
```

### Windows

```sh
podman pull quay.io/konveyor/kantra:latest && podman run --name kantra-download quay.io/konveyor/kantra:latest 1> /dev/null 2> /dev/null && podman cp kantra-download:/usr/local/bin/windows-kantra kantra && podman rm kantra-download
```

---

The above will copy the binary into your current directory. Move it to PATH for system-wide use:

```sh
sudo mv ./kantra /usr/local/bin/
```

To confirm Kantra is installed, run:

```sh
kantra --help
```

This should display the help message.

## Usage

Kantra has two subcommands - `analyze` and `transform`:


```sh
A cli tool for analysis and transformation of applications

Usage:
  kantra [command]

Available Commands:
  analyze     Analyze application source code
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  transform   Transform application source code or windup XML rules

Flags:
  -h, --help            help for kantra
      --log-level int   log level (default 5)

Use "kantra [command] --help" for more information about a command.
```

Additionally, two environment variables control the container runtime and the kantra image: `PODMAN_BIN` and `RUNNER_IMG`:
- `PODMAN_BIN`: path to your container runtime (podman or docker); ie: `PODMAN_BIN=/usr/local/bin/docker`
- `RUNNER_IMG`: the tag of the kantra image to invoke; ie: `RUNNER_IMG=localhost/kantra:latest`

### Analyze

Analyze allows running source code and binary analysis using [analyzer-lsp](https://github.com/konveyor/analyzer-lsp)

To run analysis on application source code, run:

```sh
kantra analyze --input=<path/to/source/code> --output=<path/to/output/dir>
```

All flags:

```sh
Analyze application source code

Usage:
  kantra analyze [flags]

Flags:
      --analyze-known-libraries   analyze known open-source libraries
  -h, --help                      help for analyze
  -i, --input string              path to application source code or a binary
      --json-output               create analysis and dependency output as json
      --list-sources              list rules for available migration sources
      --list-targets              list rules for available migration targets
  -m, --mode string               analysis mode. Must be one of 'full' or 'source-only' (default "full")
  -o, --output string             path to the directory for analysis output
      --rules stringArray         filename or directory containing rule files
      --skip-static-report        do not generate static report
  -s, --source stringArray        source technology to consider for analysis
  -t, --target stringArray        target technology to consider for analysis

Global Flags:
      --log-level int   log level (default 5)
```

### Transform

Transform has two subcommands - `openrewrite` and `rules`.

```sh
Transform application source code or windup XML rules

Usage:
  kantra transform [flags]
  kantra transform [command]

Available Commands:
  openrewrite Transform application source code using OpenRewrite recipes
  rules       Convert XML rules to YAML

Flags:
  -h, --help   help for transform

Global Flags:
      --log-level int   log level (default 5)

Use "kantra transform [command] --help" for more information about a command.
```

#### OpenRewrite

`openrewrite` subcommand allows running [OpenRewrite](https://docs.openrewrite.org/) recipes on source code.


```sh
Transform application source code using OpenRewrite recipes

Usage:
  kantra transform openrewrite [flags]

Flags:
  -g, --goal string     target goal (default "dryRun")
  -h, --help            help for openrewrite
  -i, --input string    path to application source code directory
  -l, --list-targets    list all available OpenRewrite recipes
  -t, --target string   target openrewrite recipe to use. Run --list-targets to get a list of packaged recipes.

Global Flags:
      --log-level int   log level (default 5)
```

To run `transform openrewrite` on application source code, run:

```sh
kantra transform openrewrite --input=<path/to/source/code> --target=<exactly_one_target_from_the_list>
```

#### Rules

`rules` subcommand allows converting Windup XML rules to analyzer-lsp YAML rules using [windup-shim](https://github.com/konveyor/windup-shim)

```sh
Convert XML rules to YAML

Usage:
  kantra transform rules [flags]

Flags:
  -h, --help                help for rules
  -i, --input stringArray   path to XML rule file(s) or directory
  -o, --output string       path to output directory

Global Flags:
      --log-level int   log level (default 5)
```

To run `transform rules` on application source code, run:

```sh
kantra transform rules --input=<path/to/xmlrules> --output=<path/to/output/dir>
```

## Quick Demos

Once you have kantra installed, these examples will help you run both an 
analyze and a transform command.

### Analyze

- Get the example application to run analysis on  
`git clone https://github.com/konveyor/example-applications`

- List available target technologies  
`kantra analyze --list-targets`

- Run analysis with a specified target technology  
`kantra analyze --input=<path-to/example-applications/example-1> --output=<path-to-output-dir> --target=cloud-readiness`

- Several analysis reports will have been created in your specified output path:

```sh
$ ls ./output/ -1
analysis.log
dependencies.yaml
dependency.log
output.yaml
static-report
```

`output.yaml` is the file that contains issues report.   
`static-report` contains the static HTML report.  
`dependencies.yaml`contains a dependencies report.  

### Transform

- Get the example application to transform source code  
`git clone https://github.com/ivargrimstad/jakartaee-duke`

- View available OpenRewrite recipes  
`kantra transform openrewrite --list-targets` 

- Run a recipe on the example application  
`kantra transform openrewrite --input=<path-to/jakartaee-duke> --target=jakarta-imports`

- Inspect the `jakartaee-duke` application source code diff to see the transformation  


## Code of Conduct
Refer to Konveyor's Code of Conduct [here](https://github.com/konveyor/community/blob/main/CODE_OF_CONDUCT.md).
