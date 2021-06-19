/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/images"
	"github.com/containerd/containerd/pkg/progress"
	refdocker "github.com/containerd/containerd/reference/docker"
	"github.com/containerd/nerdctl/pkg/imgutil"
	"github.com/opencontainers/image-spec/identity"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var imagesCommand = &cli.Command{
	Name:         "images",
	Usage:        "List images",
	Action:       imagesAction,
	BashComplete: imagesBashComplete,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "quiet",
			Aliases: []string{"q"},
			Usage:   "Only show numeric IDs",
		},
		&cli.BoolFlag{
			Name:  "no-trunc",
			Usage: "Don't truncate output",
		},
	},
}

func imagesAction(clicontext *cli.Context) error {
	var filters []string

	if clicontext.NArg() > 1 {
		return errors.New("cannot have more than one argument")
	}

	if clicontext.NArg() > 0 {
		canonicalRef, err := refdocker.ParseDockerRef(clicontext.Args().First())
		if err != nil {
			return err
		}
		filters = append(filters, fmt.Sprintf("name==%s", canonicalRef.String()))
	}
	client, ctx, cancel, err := newClient(clicontext)
	if err != nil {
		return err
	}
	defer cancel()

	var (
		imageStore = client.ImageService()
		cs         = client.ContentStore()
	)

	// To-do: Add support for --filter.
	imageList, err := imageStore.List(ctx, filters...)
	if err != nil {
		return err
	}

	return printImages(ctx, clicontext, client, imageList, cs)
}

func printImages(ctx context.Context, clicontext *cli.Context, client *containerd.Client, imageList []images.Image, cs content.Store) error {
	quiet := clicontext.Bool("quiet")
	noTrunc := clicontext.Bool("no-trunc")

	w := tabwriter.NewWriter(clicontext.App.Writer, 4, 8, 4, ' ', 0)
	if !quiet {
		fmt.Fprintln(w, "REPOSITORY\tTAG\tIMAGE ID\tCREATED\tSIZE")
	}

	var errs []error
	for _, img := range imageList {
		size, err := unpackedImageSize(ctx, clicontext, client, img)
		if err != nil {
			errs = append(errs, err)
		}
		repository, tag := imgutil.ParseRepoTag(img.Name)

		var digest string
		if !noTrunc {
			digest = strings.Split(img.Target.Digest.String(), ":")[1][:12]
		} else {
			digest = img.Target.Digest.String()
		}

		if quiet {
			if _, err := fmt.Fprintf(w, "%s\n", digest); err != nil {
				return err
			}
			continue
		}

		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			repository,
			tag,
			digest,
			timeSinceInHuman(img.CreatedAt),
			progress.Bytes(size),
		); err != nil {
			return err
		}
	}
	if len(errs) > 0 {
		logrus.Warn("failed to compute image(s) size")
	}
	return w.Flush()
}

func imagesBashComplete(clicontext *cli.Context) {
	coco := parseCompletionContext(clicontext)
	if coco.boring || coco.flagTakesValue {
		defaultBashComplete(clicontext)
		return
	}
	// show image names
	bashCompleteImageNames(clicontext)
}

func unpackedImageSize(ctx context.Context, clicontext *cli.Context, client *containerd.Client, i images.Image) (int64, error) {
	img := containerd.NewImage(client, i)

	diffIDs, err := img.RootFS(ctx)
	if err != nil {
		return 0, err
	}
	chainID := identity.ChainID(diffIDs).String()
	s := client.SnapshotService(clicontext.String("snapshotter"))
	usage, err := s.Usage(ctx, chainID)
	if err != nil {
		return 0, err
	}

	return usage.Size, nil
}
