package util

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"github.com/suborbital/atmo/directive"
	"github.com/suborbital/subo/subo/context"
)

// ErrTemplateMissing and others are template related errors
var ErrTemplateMissing = errors.New("template missing")

// Mkdir creates a new directory to contain a runnable
func Mkdir(cwd, name string) (string, error) {
	path := filepath.Join(cwd, name)

	if err := os.Mkdir(path, 0777); err != nil {
		return "", errors.Wrap(err, "failed to Mkdir")
	}

	return path, nil
}

type tmplData struct {
	directive.Runnable
	NameCaps  string
	NameCamel string
}

func UpdateTemplates(bctx *context.BuildContext, name, branch string) (string, error) {
	LogStart("downloading templates")

	branchDirName := fmt.Sprintf("subo-%s", strings.ReplaceAll(branch, "/", "-"))

	templateRootPath, err := TemplateRootDir()
	if err != nil {
		return "", errors.Wrap(err, "failed to TemplateDir")
	}

	filepath, err := downloadZip(branch, templateRootPath)
	if err != nil {
		return "", errors.Wrap(err, "🚫 failed to downloadZip for templates")
	}

	// tmplPath may be different than the default if a custom URL was provided
	tmplPath, err := extractZip(filepath, templateRootPath, branchDirName)
	if err != nil {
		return "", errors.Wrap(err, "🚫 failed to extractZip for templates")
	}

	LogDone("templates downloaded")

	return tmplPath, nil
}

// ExecRunnableTmpl copies a template
func ExecRunnableTmpl(cwd, name, templatesPath string, runnable *directive.Runnable) error {
	nameCamel := ""
	nameParts := strings.Split(runnable.Name, "-")
	for _, part := range nameParts {
		nameCamel += strings.ToUpper(string(part[0]))
		nameCamel += string(part[1:])
	}

	templateData := tmplData{
		Runnable:  *runnable,
		NameCaps:  strings.ToUpper(strings.Replace(runnable.Name, "-", "", -1)),
		NameCamel: nameCamel,
	}

	return ExecTmplDir(cwd, name, templatesPath, runnable.Lang, templateData)
}

// ExecTmplDir copies a generic templated directory
func ExecTmplDir(cwd, name, templatesPath, tmplName string, templateData interface{}) error {
	templatePath := filepath.Join(templatesPath, tmplName)
	targetPath := filepath.Join(cwd, name)

	if _, err := os.Stat(templatePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrTemplateMissing
		}

		return errors.Wrap(err, "failed to Stat template directory")
	}

	var err error = filepath.Walk(templatePath, func(path string, info os.FileInfo, err error) error {
		var relPath string = strings.Replace(path, templatePath, "", 1)
		if relPath == "" {
			return nil
		}

		targetRelPath := relPath
		if strings.Contains(relPath, ".tmpl") {
			tmpl, err := template.New("tmpl").Parse(strings.Replace(relPath, ".tmpl", "", -1))
			if err != nil {
				return errors.Wrapf(err, "failed to parse template directory name %s", info.Name())
			}

			builder := &strings.Builder{}
			if err := tmpl.Execute(builder, templateData); err != nil {
				return errors.Wrapf(err, "failed to Execute template for %s", info.Name())
			}

			targetRelPath = builder.String()
		}

		if info.IsDir() {
			return os.Mkdir(filepath.Join(targetPath, targetRelPath), 0755)
		}

		var data, err1 = ioutil.ReadFile(filepath.Join(templatePath, relPath))
		if err1 != nil {
			return err1
		}

		if strings.HasSuffix(info.Name(), ".tmpl") {
			tmpl, err := template.New("tmpl").Parse(string(data))
			if err != nil {
				return errors.Wrapf(err, "failed to parse template file %s", info.Name())
			}

			builder := &strings.Builder{}
			if err := tmpl.Execute(builder, templateData); err != nil {
				return errors.Wrapf(err, "failed to Execute template for %s", info.Name())
			}

			data = []byte(builder.String())
		}

		return ioutil.WriteFile(filepath.Join(targetPath, targetRelPath), data, 0777)
	})

	return err
}

// downloadZip downloads a ZIP from a particular branch of the Subo repo
func downloadZip(branch, targetPath string) (string, error) {
	url := fmt.Sprintf("https://github.com/suborbital/subo/archive/%s.zip", branch)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", errors.Wrap(err, "failed to NewRequest")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "failed to Do request")
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("response was non-200: %d", resp.StatusCode)
	}

	filePath := filepath.Join(targetPath, "subo.zip")

	// check if the zip already exists, and delete it if it does
	if _, err := os.Stat(filePath); err == nil {
		if err := os.Remove(filePath); err != nil {
			return "", errors.Wrap(err, "failed to delete exising templates zip")
		}
	}

	if err := os.MkdirAll(targetPath, os.ModePerm); err != nil {
		return "", errors.Wrap(err, "failed to MkdirAll")
	}

	file, err := os.Create(filePath)
	if err != nil {
		return "", errors.Wrap(err, "failed to Open file")
	}

	defer resp.Body.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", errors.Wrap(err, "failed to Copy data to file")
	}

	return filePath, nil
}

// extractZip extracts a ZIP file
func extractZip(filePath, destPath, branchDirName string) (string, error) {
	escapedFilepath := strings.ReplaceAll(filePath, " ", "\\ ")
	escapedDestPath := strings.ReplaceAll(destPath, " ", "\\ ") + string(filepath.Separator)

	existingPath := filepath.Join(destPath, branchDirName)

	if _, err := os.Stat(existingPath); err == nil {
		if err := os.RemoveAll(existingPath); err != nil {
			return "", errors.Wrap(err, "failed to RemoveAll old templates")
		}
	}

	if _, _, err := Run(fmt.Sprintf("unzip -q %s -d %s", escapedFilepath, escapedDestPath)); err != nil {
		return "", errors.Wrap(err, "failed to Run unzip")
	}

	return filepath.Join(existingPath, "templates"), nil
}
