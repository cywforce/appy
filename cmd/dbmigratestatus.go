package cmd

import (
	"fmt"

	"github.com/appist/appy/record"
	"github.com/appist/appy/support"
	"github.com/bndr/gotabulate"
)

func newDBMigrateStatusCommand(config *support.Config, dbManager *record.Engine, logger *support.Logger) *Command {
	return &Command{
		Use:   "db:migrate:status",
		Short: "List all the database migration status(default: all, use --database to specify the target database) for the current environment",
		Run: func(cmd *Command, args []string) {
			if len(config.Errors()) > 0 {
				logger.Fatal(config.Errors()[0])
			}

			if len(dbManager.Errors()) > 0 {
				logger.Fatal(dbManager.Errors()[0])
			}

			if len(dbManager.Databases()) < 1 {
				logger.Fatalf("No database is defined in '%s'", config.Path())
			}

			for name, db := range dbManager.Databases() {
				if db.Config().Replica {
					continue
				}

				err := db.Connect()
				if err != nil {
					logger.Fatal(err)
				}
				defer db.Close()

				status, err := db.MigrateStatus()
				if err != nil {
					logger.Fatal(err)
				}

				fmt.Println()
				fmt.Printf("database: %s\n", name)
				fmt.Println()

				table := gotabulate.Create(status)
				table.SetAlign("left")
				table.SetHeaders([]string{"Status", "Migration ID", "Migration File"})
				fmt.Println(table.Render("simple"))
			}
		},
	}
}
