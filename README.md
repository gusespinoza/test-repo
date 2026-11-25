# Angular + Go

Base de un sistema de tickets con frontend Angular, API Go, PostgreSQL y Redis.

## Requisitos

- Node.js 20.19 o superior y npm
- Go 1.26 o superior
- Docker Compose

## Servicios de datos

Copia `.env.example` a `.env` y cambia `POSTGRES_PASSWORD` por un valor local. Luego inicia PostgreSQL y Redis:

```powershell
docker compose up -d
```

Aplica el esquema inicial:

```powershell
Get-Content .\backend\internal\auth\repository\schema.sql | docker compose exec -T postgres psql -U tickets -d tickets
Get-Content .\backend\internal\events\repository\schema.sql | docker compose exec -T postgres psql -U tickets -d tickets
Get-Content .\backend\internal\bookings\repository\schema.sql | docker compose exec -T postgres psql -U tickets -d tickets
```

El esquema de autenticación crea `auth_users` y `auth_refresh_tokens`; el esquema de eventos crea `events`. Las cuentas deben crearse mediante un flujo administrativo seguro y almacenar contraseñas con bcrypt.

## Eventos

- `GET /api/events?page=1&limit=20` lista eventos con paginación (el límite máximo es 100).
- `GET /api/events/{id}` devuelve un evento.
- `POST /api/events` y `PUT /api/events/{id}` requieren un JWT con rol `admin`.
- Para crear o actualizar se exige `title` y `venue` no vacíos, `starts_at` en formato RFC3339 y `capacity` mayor que cero. `id` y `created_at` los controla el servidor y no se aceptan en el cuerpo.
- El esquema de PostgreSQL del módulo está en `backend/internal/events/repository/schema.sql`.

## Reservas

- `POST /api/bookings` requiere autenticación y recibe `event_id`, `quantity` e `idempotency_key`. Una misma clave por usuario devuelve la orden existente.
- Las reservas `pending` vencen a los 10 minutos. Redis mantiene el contador temporal `booking:lock:<event_id>`; PostgreSQL serializa reservas y confirmaciones por evento para no sobrepasar la capacidad.
- `POST /api/bookings/{id}/confirm` y `DELETE /api/bookings/{id}` requieren ser dueño de la orden. Confirmar registra las entradas vendidas en PostgreSQL; cancelar libera las entradas vendidas si corresponde y libera la retención de Redis.
- `GET /api/bookings/{id}` permite consultar al dueño o a un usuario con rol `admin`.
- Aplica `backend/internal/bookings/repository/schema.sql` después de los esquemas de autenticación y eventos. Este añade el contador de ventas a `events` y crea `orders`.
- El test de integración de reservas usa PostgreSQL y Redis efímeros: `go test -tags=integration ./internal/bookings/...`.

## Desarrollo

En una terminal, configura las variables e inicia la API:

```powershell
$env:DATABASE_URL = "postgres://tickets:change-this-local-password@localhost:5432/tickets?sslmode=disable"
$env:REDIS_ADDR = "localhost:6379"
$env:JWT_SECRET = "reemplazar-por-un-secreto-aleatorio-de-al-menos-32-bytes"
$env:JWT_ISSUER = "angular-go-starter"
Set-Location .\backend
go run .
```

En otra terminal, inicia Angular:

```powershell
Set-Location .\frontend
npm start
```

Abre <http://localhost:4200>. Angular reenvía `/api` al backend en `localhost:8080`.

## Autenticación

- `POST /api/auth/login` recibe `{"email":"...","password":"..."}` y devuelve `access_token`, `token_type`, `expires_in` y el `role` cargado desde PostgreSQL. El refresh token opaco se guarda solo en una cookie `HttpOnly`, `Secure`, `SameSite=Lax`.
- El login limita 5 intentos por minuto por IP y 10 por minuto por email usando Redis. Si se supera un límite, responde `429` con `Retry-After`; si Redis falla durante la comprobación, lo registra y permite continuar.
- Los JWT HS256 duran 15 minutos. El secreto se valida al arrancar y debe tener al menos 32 bytes.
- `POST /api/auth/refresh` rota el refresh token de 7 días: guarda únicamente su hash y reutilizar uno ya rotado revoca toda su familia y responde `401`.
- `POST /api/auth/logout` requiere `Authorization: Bearer <token>`, revoca la familia del refresh token y limpia la cookie.
- El middleware verifica firma, algoritmo, issuer, expiración y revocación. Usa `RequireRoles` para proteger endpoints por rol; las comprobaciones de permisos siempre deben hacerse en el backend.
- Angular mantiene el access token solo en memoria. El interceptor adjunta el token, comparte una sola llamada de refresh entre respuestas `401` simultáneas y reintenta las peticiones pendientes.

Compila el frontend con `npm run build` desde `frontend` y valida el backend con `go test ./...` desde `backend`. La prueba de integración de refresh requiere Docker en ejecución:

```powershell
Set-Location .\backend
go test -tags=integration ./internal/auth/...
```
