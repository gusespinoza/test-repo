package cleanup

import (
	"context"
	"database/sql"
	"log"
	"time"
)

const cancelExpiredOrdersQuery = `
UPDATE orders
SET status = 'cancelled', updated_at = now()
WHERE status = 'pending' AND expires_at < now()`

func StartCleanupWorker(ctx context.Context, db *sql.DB) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := db.ExecContext(ctx, cancelExpiredOrdersQuery)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					log.Printf("limpiar órdenes expiradas: %v", err)
					continue
				}

				rowsAffected, err := result.RowsAffected()
				if err != nil {
					log.Printf("obtener cantidad de órdenes canceladas: %v", err)
					continue
				}
				log.Printf("órdenes expiradas canceladas: %d", rowsAffected)
			}
		}
	}()
}
