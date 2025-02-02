package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/upsidr/importer/internal/parse"
)

var (
	previewCliCmd = &cli.Command{
		Name:      "preview",
		UsageText: rootCmdName + " preview [filename]",
		Usage:     "Shows a preview of Importer update and purge results",
		Description: `
` + "`preview`" + ` command processes the provided file and gives you a quick preview.

This allows you to find what the file looks like after ` + "`update`" + ` or ` + "`purge`" + `.
`,
		Action: executePreview,
		// TODO: Add flags to see only specific preview (e.g. `importer preview file --update` for update only view)
		// TODO: Add support for diff preview
	}
)

func executePreview(ctx *cli.Context) error {
	args := ctx.Args()
	// TODO: add some util func to hande all common error cases
	if args.Len() != 1 {
		return errors.New("error: incorrect argument, you can only pass in 1 argument")
	}

	arg := args.First()
	if err := preview(arg); err != nil {
		return fmt.Errorf("error: handling preview, %v", err)
	}

	return nil
}

func preview(fileName string) error {
	f, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer f.Close()

	file, err := parse.Parse(fileName, f)
	if err != nil {
		return err
	}

	err = file.ProcessMarkers()
	if err != nil {
		return err
	}

	file.PrintDebugAll()

	fileLen := len(fileName) + 2
	fmt.Printf(`You can replace the file content with either of the commands below:

  importer update %-*s   Replace the file content with the Importer processed file.
  importer purge %-*s    Replace the file content by removing all data between marker pairs.

You can find more with 'importer help'
`, fileLen, fileName, fileLen, fileName)

	return nil
}
