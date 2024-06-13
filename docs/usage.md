## Output


#### Default rules:

- the analyze command runs against a set of packaged rules [here](https://github.com/konveyor/rulesets/)
- `--label-selector` and/or `--target` can filter these rules
- `--rules` can be provided to run analyze on rules outside of this set

#### `--rules` + `--target`

- In kantra, if a rule is given but it **does not** have a target 
  label, the given rule will not match. 
    - You must add the target label to the custom rule and specify the `--target`
     in order to run this rule.


## Provider Options

The supported providers have several options to utilize. Examples of the available  
options can be found [here](../java.json.sample) and [here](../golang.json.sample). To read about each of these options,    
see the analyzer provider [documentation](https://github.com/konveyor/analyzer-lsp/blob/main/docs/providers.md).  

Kantra will look for these options at:  
- Linux: `$XDG_CONFIG_HOME/.kantra/<provider_name>.json` and then `$HOME/.kantra/<provider_name>.json` 
- MacOS: `$HOME/.kantra/<provider_name>.json` 
- Windows: `%USERPROFILE%/.kantra/<provider_name>.json`

Current supported providers are:
- java
- golang
- python
- nodejs
