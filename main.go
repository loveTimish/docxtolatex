package main

import (
	"errors"
	"fmt"
	"github.com/urfave/cli"
	"github.com/zhexiao/mtef-go/docx"
	"github.com/zhexiao/mtef-go/eqn"
	"log"
	"os"
	"time"
)

func main() {
	var filepath, docxDocument, output, configPath string
	var writeReport bool

	app := cli.NewApp()
	app.Name = "Mtef"
	app.Usage = "Convert MSDocx Mathtype Ole object to Latex code or extract docx equations into a .tex document"
	app.Version = "3.1"
	app.EnableBashCompletion = true

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "filepath, f",
			Usage:       "Mathtype OLE object filepath",
			Destination: &filepath,
		},
		cli.StringFlag{
			Name:        "wordDocx, w",
			Usage:       "Office Word .docx document",
			Destination: &docxDocument,
		},
		cli.StringFlag{
			Name:        "output, o",
			Usage:       "Target output folder (defaults to <docx name>)",
			Destination: &output,
		},
		cli.StringFlag{
			Name:        "config, c",
			Usage:       "Optional JSON config file for document styles, image rendering, and report settings",
			Destination: &configPath,
		},
		cli.BoolFlag{
			Name:        "report, r",
			Usage:       "Write a JSON conversion report alongside the generated .tex output",
			Destination: &writeReport,
		},
	}

	app.Action = func(c *cli.Context) error {
		if filepath != "" {
			if _, err := os.Stat(filepath); os.IsNotExist(err) {
				return fmt.Errorf("file does not exist: %s", filepath)
			}

			latex, err := eqn.ConvertFile(filepath)
			if err != nil {
				return err
			}
			fmt.Println(latex)
			return nil
		}

		if docxDocument != "" {
			if _, err := os.Stat(docxDocument); os.IsNotExist(err) {
				return fmt.Errorf("file does not exist: %s", docxDocument)
			}

			cfg, err := docx.LoadConfig(configPath)
			if err != nil {
				return err
			}

			converter := docx.Converter{
				Source:      docxDocument,
				Output:      output,
				Config:      cfg,
				WriteReport: writeReport,
			}

			// capture and discard noise prints from underlying eqn package / stdlib logger
			stdout := os.Stdout
			stderr := os.Stderr
			prevLogWriter := log.Writer()
			devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0644)
			if err == nil {
				os.Stdout = devNull
				os.Stderr = devNull
				log.SetOutput(devNull)
			}

			start := time.Now()
			count, err := converter.Convert()

			// restore stdout/stderr before printing our final output
			os.Stdout = stdout
			os.Stderr = stderr
			log.SetOutput(prevLogWriter)
			if devNull != nil {
				devNull.Close()
			}

			if err == nil {
				fmt.Printf("Converted %d equations to %s in %s\n", count, converter.Output, time.Since(start))
			}

			return err
		}

		return errors.New("please specify either --filepath for a single OLE object or --wordDocx for a .docx file")
	}

	if err := app.Run(os.Args); err != nil {
		log.Panic(err)
	}
}
