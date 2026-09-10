package main

import (
	"errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/hooks/spam_block/tools"
	"git.pcux.cn/xuans/Smail/models"
	"github.com/spf13/cast"
	"os"
	"strings"
)

func getType(emailId int) (int, error) {
	var ue models.UserEmail
	has, err := db.Instance.Table(&ue).Where("email_id = ?", emailId).Limit(1).Get(&ue)
	if err != nil {
		return 0, err
	}
	if !has {
		return 0, nil
	}
	if ue.Status == 3 {
		return 2, nil
	}
	if ue.Status == 5 {
		return 1, nil
	}
	return 0, nil
}

func main() {
	args := os.Args

	var id int
	if len(args) >= 2 {
		id = cast.ToInt(args[1])
	}

	if err := config.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "configuration initialization failed: %v\n", err)
		os.Exit(1)
	}
	err := db.Init("test")
	if err != nil {
		fmt.Fprintf(os.Stderr, "database initialization failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("文件第一列是分类，0表示正常邮件，1表示垃圾邮件，2表示诈骗邮件")

	if err := exportEmails(id); err != nil {
		fmt.Fprintf(os.Stderr, "导出失败: %v\n", err)
		os.Exit(1)
	}
}

func exportEmails(id int) (retErr error) {
	file, err := os.OpenFile("data.csv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open export file: %w", err)
	}
	defer func() {
		retErr = errors.Join(retErr, file.Close())
	}()

	var start int
	for {
		var emails []models.Email
		query := db.Instance.Table(&models.Email{}).OrderBy("id")
		if id > 0 {
			query = query.Where("id = ?", id).Limit(1)
		} else {
			query = query.Where("id > ?", start).Limit(500)
		}
		if err := query.Find(&emails); err != nil {
			return fmt.Errorf("query emails: %w", err)
		}
		if len(emails) == 0 {
			break
		}
		for _, email := range emails {
			start = email.Id
			content := tools.Trim(tools.TrimHtml(email.Html.String))
			if content == "" {
				content = tools.Trim(email.Text.String)
			}
			category, err := getType(email.Id)
			if err != nil {
				return fmt.Errorf("query email classification: %w", err)
			}
			line := fmt.Sprintf("%d \t%s %s\n", category, safeExportText(email.Subject), safeExportText(content))
			if _, err := file.WriteString(line); err != nil {
				return fmt.Errorf("write export file: %w", err)
			}
		}
		if id > 0 {
			break
		}
	}
	return nil
}

func safeExportText(value string) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(value)
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.ContainsRune("=+-@", rune(value[0])) {
		return "'" + value
	}
	return value
}
