package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/apex/log"
	"github.com/spf13/cobra"
)

type openRewriteCommand struct {
	listTargets bool
	input       string
	target      string
	goal        string
	miscOpts    string
}

func NewOpenRewriteCommand() *cobra.Command {
	openRewriteCmd := &openRewriteCommand{}

	openRewriteCommand := &cobra.Command{
		Use: "openrewrite",

		Short: "Transform application source code using OpenRewrite recipes",
		PreRun: func(cmd *cobra.Command, args []string) {
			if !cmd.Flags().Lookup("list-targets").Changed {
				cmd.MarkFlagRequired("input")
				cmd.MarkFlagRequired("target")
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			err := openRewriteCmd.Validate()
			if err != nil {
				return err
			}
			err = openRewriteCmd.Run(cmd.Context())
			if err != nil {
				log.Errorf("failed to execute openrewrite command", err)
				return err
			}
			return nil
		},
	}
	openRewriteCommand.Flags().BoolVarP(&openRewriteCmd.listTargets, "list-targets", "l", false, "list all available OpenRewrite recipes")
	openRewriteCommand.Flags().StringVarP(&openRewriteCmd.target, "target", "t", "", "target openrewrite recipe to use. Run --list-targets to get a list of packaged recipes.")
	openRewriteCommand.Flags().StringVarP(&openRewriteCmd.goal, "goal", "g", "dryRun", "target goal")
	openRewriteCommand.Flags().StringVarP(&openRewriteCmd.input, "input", "i", "", "path to application source code directory")

	return openRewriteCommand
}

func (o *openRewriteCommand) Validate() error {
	if o.listTargets {
		return nil
	}

	stat, err := os.Stat(o.input)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		log.Errorf("input path %s is not a directory", o.input)
		return err
	}

	if o.target == "" {
		return fmt.Errorf("target recipe must be specified")
	}

	if _, found := recipes[o.target]; !found {
		return fmt.Errorf("unsupported target recipe. use --list-targets to get list of all recipes")
	}
	return nil
}

type recipe struct {
	names       []string
	path        string
	description string
}

var recipes = map[string]recipe{
	"eap8-xml": {
		names:       []string{"org.jboss.windup.eap8.FacesWebXml"},
		path:        "eap8/xml/rewrite.yml",
		description: "Transform Faces Web XML for EAP8 migration",
	},
	"jakarta-xml": {
		names:       []string{"org.jboss.windup.jakarta.javax.PersistenceXml"},
		path:        "jakarta/javax/xml/rewrite.yml",
		description: "Transform Persistence XML for Jakarta migration",
	},
	"jakarta-bootstrapping": {
		names:       []string{"org.jboss.windup.jakarta.javax.BootstrappingFiles"},
		path:        "jakarta/javax/bootstrapping/rewrite.yml",
		description: "Transform bootstrapping files for Jakarta migration",
	},
	"jakarta-imports": {
		names:       []string{"org.jboss.windup.JavaxToJakarta"},
		path:        "jakarta/javax/imports/rewrite.yml",
		description: "Transform dependencies and imports for Jakarta migration",
	},
	"quarkus-properties": {
		names:       []string{"org.jboss.windup.sb-quarkus.Properties"},
		path:        "quarkus/springboot/properties/rewrite.yml",
		description: "Migrate Springboot properties to Quarkus",
	},
}

func (o *openRewriteCommand) Run(ctx context.Context) error {
	if o.listTargets {
		fmt.Printf("%-20s\t%s\n", "NAME", "DESCRIPTION")
		for name, recipe := range recipes {
			fmt.Printf("%-20s\t%s\n", name, recipe.description)
		}
		return nil
	}

	volumes := map[string]string{
		o.input: InputPath,
	}
	args := []string{
		"-U", "org.openrewrite.maven:rewrite-maven-plugin:run",
		fmt.Sprintf("-Drewrite.configLocation=%s/%s",
			OpenRewriteRecipesPath, recipes[o.target].path),
		fmt.Sprintf("-Drewrite.activeRecipes=%s",
			strings.Join(recipes[o.target].names, ",")),
	}
	cmd := NewContainerCommand(
		ctx,
		WithEntrypointArgs(args...),
		WithEntrypointBin("/usr/bin/mvn"),
		WithVolumes(volumes),
		WithWorkDir(InputPath),
	)
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}
