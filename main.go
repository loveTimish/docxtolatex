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
	var filepath, docxDocument, output string

	app := cli.NewApp()
	app.Name = "Mtef"
	app.Usage = "Convert MSDocx Mathtype Ole object to Latex code or extract docx equations into a .tex document"
	app.Version = "3.0"
	app.EnableBashCompletion = true

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "filepath, f",
			Usage:       "Mathtype Ole object filepath",
			Destination: &filepath,
		},
		cli.StringFlag{
			Name:        "wordDocx, w",
			Usage:       "Office word docx documents",
			Destination: &docxDocument,
		},
		cli.StringFlag{
			Name:        "output, o",
			Usage:       "Target output folder (defaults to <docx name>)",
			Destination: &output,
		},
	}

	app.Action = func(c *cli.Context) error {
		if filepath != "" {
			if _, err := os.Stat(filepath); os.IsNotExist(err) {
				fmt.Println("File not exist!!!!")
				return nil
			}

			latex := eqn.Convert(filepath)
			fmt.Println(latex)
			return nil
		}

		if docxDocument != "" {
			if _, err := os.Stat(docxDocument); os.IsNotExist(err) {
				fmt.Println("File not exist!!!!")
				return nil
			}

			converter := docx.Converter{
				Source: docxDocument,
				Output: output,
			}

			start := time.Now()
			count, err := converter.Convert()
			if err != nil {
				return err
			}

			fmt.Printf("Converted %d equations to %s in %s\n", count, converter.Output, time.Since(start))
			return nil
		}

		return errors.New("please specify either --filepath for a single OLE object or --wordDocx for a .docx file")
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Panic(err)
	}
}
