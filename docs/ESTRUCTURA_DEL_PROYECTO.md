# Estructura del proyecto

## Resumen

Este repositorio es la base de un sistema de tickets con un frontend Angular y un backend Go organizado como monolito modular. PostgreSQL almacena las cuentas, sus roles, los eventos y las órdenes; Redis mantiene los tokens JWT revocados, los contadores de intentos de login y las retenciones temporales de entradas.

Durante el desarrollo, Angular sirve la aplicación en el puerto `4200` y reenvía las peticiones `/api/` al backend Go en el puerto `8080`. PostgreSQL y Redis se pueden iniciar con Docker Compose.

## Árbol de carpetas

```text
.
├── backend/
│   ├── main.go
│   ├── go.mod
│   ├── go.sum
│   └── internal/
│       ├── auth/
│       │   ├── domain/
│       │   │   └── user.go
│       │   ├── handlers/
│       │   │   ├── handler.go
│       │   │   └── handler_test.go
│       │   ├── middleware/
│       │   │   ├── jwt.go
│       │   │   └── jwt_test.go
│       │   ├── repository/
│       │   │   ├── postgres.go
│       │   │   ├── refresh_tokens.go
│       │   │   ├── redis_revocations.go
│       │   │   └── schema.sql
│       │   ├── service/
│       │   │   ├── service.go
│       │   │   └── service_test.go
│       │   ├── token/
│       │   │   └── manager.go
│       │   └── integration/
│       │       └── refresh_tokens_test.go
│       ├── ratelimit/
│       │   ├── middleware.go
│       │   └── middleware_test.go
│       ├── cleanup/
│       │   └── worker.go
│       ├── events/
│           ├── domain/
│           │   └── event.go
│           ├── handlers/
│           │   ├── handler.go
│           │   └── handler_test.go
│           ├── repository/
│           │   ├── postgres.go
│           │   └── schema.sql
│           └── service/
│           │   ├── service.go
│           │   └── service_test.go
│       └── bookings/
│           ├── domain/
│           │   └── booking.go
│           ├── handlers/
│           │   └── handler.go
│           ├── integration/
│           │   └── bookings_integration_test.go
│           ├── repository/
│           │   ├── postgres.go
│           │   ├── redis_reservations.go
│           │   └── schema.sql
│           └── service/
│               └── service.go
├── frontend/
│   ├── angular.json
│   ├── package.json
│   ├── package-lock.json
│   ├── proxy.conf.json
│   ├── public/
│   │   └── favicon.ico
│   └── src/
│       ├── app/
│       │   ├── features/
│       │   │   ├── bookings/
│       │   │   │   ├── booking-checkout.component.ts
│       │   │   │   ├── booking-checkout.component.html
│       │   │   │   ├── booking-checkout.component.css
│       │   │   │   ├── booking-selection.component.ts
│       │   │   │   ├── booking-selection.component.html
│       │   │   │   ├── booking-selection.component.css
│       │   │   │   ├── bookings.models.ts
│       │   │   │   └── bookings.service.ts
│       │   │   └── events/
│       │   │       ├── events.models.ts
│       │   │       ├── events.service.ts
│       │   │       ├── events-list.component.ts
│       │   │       ├── events-list.component.html
│       │   │       ├── events-list.component.css
│       │   │       ├── event-detail.component.ts
│       │   │       ├── event-detail.component.html
│       │   │       └── event-detail.component.css
│       │   ├── core/
│       │   │   └── auth/
│       │   │       ├── auth.interceptor.ts
│       │   │       ├── auth.interceptor.spec.ts
│       │   │       ├── auth.models.ts
│       │   │       └── auth.service.ts
│       │   ├── app.config.ts
│       │   ├── app.routes.ts
│       │   ├── app.ts
│       │   ├── app.html
│       │   └── app.css
│       ├── main.ts
│       ├── index.html
│       └── styles.css
├── docs/
│   └── ESTRUCTURA_DEL_PROYECTO.md
├── .env.example
├── .gitignore
├── docker-compose.yml
└── README.md
```

Angular CLI y npm también generan archivos de configuración, como `tsconfig*.json`, y dependencias en `node_modules/`; se omitieron del árbol para destacar el código de la aplicación.

## Backend: monolito modular

El punto de composición está en `backend/main.go`: configura conexiones, construye las dependencias de los módulos `auth`, `events` y `bookings` y registra las rutas HTTP. El rate limiter transversal vive en `backend/internal/ratelimit`; el worker de `backend/internal/cleanup` cancela periódicamente las órdenes pendientes expiradas y se detiene durante el apagado ordenado. Mantener los módulos bajo `internal` evita que otras aplicaciones importen accidentalmente estos paquetes como una API pública.

- **`domain/`**: modelo de usuario y errores del dominio.
- **`handlers/`**: adaptadores HTTP. Decodifican login, traducen errores a códigos HTTP y producen la respuesta JSON.
- **`service/`**: casos de uso. Busca la cuenta, verifica la contraseña con bcrypt, crea access y refresh tokens, rota sesiones y coordina el logout.
- **`repository/`**: acceso a PostgreSQL y Redis. `postgres.go` consulta `auth_users`; `refresh_tokens.go` crea, rota y revoca familias de refresh tokens en transacciones; `redis_revocations.go` guarda los `jti` revocados; `schema.sql` define las tablas e índices.
- **`token/`**: emisión y validación de JWT HS256, con `issuer`, `subject`, rol, identificador único y expiración.
- **`middleware/`**: valida el esquema `Bearer`, la firma y los claims; consulta Redis para rechazar tokens revocados y expone helpers de autenticación y autorización por rol.
- **`internal/ratelimit/`**: aplica límites atómicos con Redis exclusivamente a `POST /api/auth/login`: 5 intentos por IP y 10 por email en 60 segundos. Si un límite se excede, responde `429` con `Retry-After`; si Redis falla, registra el error y deja pasar la petición.
- **`internal/cleanup/`**: cancela periódicamente en PostgreSQL las órdenes `pending` cuya expiración ya pasó; Redis libera los locks por TTL sin intervención del worker.

Los endpoints actuales son `POST /api/auth/login`, `POST /api/auth/refresh` y `POST /api/auth/logout`. Login y refresh devuelven el access token, su duración y el rol; el refresh token se transporta únicamente en una cookie `HttpOnly`, `Secure`, `SameSite=Lax`. Los refresh tokens viven 7 días, se almacenan como hashes en `auth_refresh_tokens` y cada refresh rota el token dentro de la misma familia. Reutilizar un token revocado invalida toda la familia y responde `401`. Logout requiere un access token válido, revoca la familia y limpia la cookie. La prueba de integración con PostgreSQL efímero está en `internal/auth/integration/`. Las cuentas se provisionan fuera de este módulo; no hay registro público ni recuperación de contraseña.

El módulo `backend/internal/events` organiza la gestión de eventos por dominio, handlers HTTP, servicio y repositorio PostgreSQL. El dominio declara `Event` y los contratos paginados; los handlers validan las peticiones y traducen errores a HTTP; el servicio valida entradas y coordina casos de uso; el repositorio consulta y actualiza PostgreSQL. `GET /api/events` permite paginar con `page` y `limit` (por defecto 1 y 20; máximo 100); `GET /api/events/{id}` devuelve el detalle. `GET /api/admin/events` requiere rol `admin` y añade el campo `sold` a cada evento. Crear y actualizar con `POST /api/events` y `PUT /api/events/{id}` también requiere ese rol. Su tabla se define en `backend/internal/events/repository/schema.sql`; el contador de ventas se añade en el esquema de bookings.

El módulo `backend/internal/bookings` crea órdenes idempotentes con estado `pending`, mantiene las retenciones temporales en Redis y confirma/cancela con transacciones PostgreSQL. Todas las rutas requieren el middleware de autenticación; confirmar y cancelar solo los puede realizar el dueño, mientras que la consulta también permite el rol `admin`. `GET /api/admin/bookings` exige rol `admin`, pagina las órdenes (máximo 100 por página) y usa JOINs para incluir el email del comprador y el título del evento. PostgreSQL bloquea la fila del evento para coordinar capacidad y ventas con el contador Redis. Su esquema añade `events.sold` y crea `orders`; aplícalo después de los esquemas de `auth` y `events`.

## Frontend

La aplicación Angular vive en `frontend/src`. `app.config.ts` configura el router y los proveedores HTTP; `app.routes.ts` carga de forma lazy el listado y el detalle de eventos; `app.ts`, `app.html` y `app.css` implementan el shell de navegación.

La funcionalidad de eventos vive en `app/features/events/`:

- **`events.models.ts`**: contratos TypeScript que reflejan los campos JSON del backend, incluidos `starts_at`, `created_at` y `updated_at`.
- **`events.service.ts`**: llamadas HTTP para listar, consultar, crear y actualizar eventos.
- **`events-list.component.*`**: listado con navegación de páginas.
- **`event-detail.component.*`**: consulta y presentación del detalle de un evento.

La funcionalidad de reservas vive en `app/features/bookings/`: `bookings.models.ts` replica el contrato JSON de `Order`, `bookings.service.ts` encapsula las llamadas a la API, `booking-selection.component.*` permite elegir evento y cantidad, y `booking-checkout.component.*` muestra la reserva pendiente con una cuenta regresiva hasta `expires_at`.

El área de administración vive en `app/features/admin/` y carga sus componentes bajo demanda. `AdminEventsComponent` presenta eventos y ventas, y permite crear eventos; `AdminBookingsComponent` muestra las órdenes con paginación. `AdminGuard` verifica que el rol en memoria sea `admin` antes de activar las rutas.

El código común de autenticación está en `app/core/auth/`:

- **`auth.models.ts`**: contratos de login y roles compartidos con la API.
- **`auth.service.ts`**: envía login, refresh y logout con credenciales, conserva el access token y el rol en memoria, y comparte una única petición de refresh concurrente.
- **`auth.interceptor.ts`**: agrega `Authorization: Bearer <token>`, detecta respuestas `401`, solicita un refresh y reintenta las peticiones pendientes.

`frontend/proxy.conf.json` conecta las llamadas `/api/` con Go durante el desarrollo. El interceptor facilita adjuntar credenciales, pero no sustituye la autorización del backend.

## Datos y configuración local

- **PostgreSQL**: almacena `auth_users`, `auth_refresh_tokens`, `events` y `orders`. Los esquemas modulares están en `backend/internal/auth/repository/schema.sql`, `backend/internal/events/repository/schema.sql` y `backend/internal/bookings/repository/schema.sql`; aplícalos en ese orden.
- **Redis**: utiliza claves `auth:revoked:<jti>` con expiración, contadores para limitar intentos de login y `booking:lock:<event_id>` con TTL de 10 minutos para retener entradas pendientes.
- **`docker-compose.yml`**: define los servicios locales de PostgreSQL y Redis.
- **`.env.example`**: variables de ejemplo para Compose. No contiene configuración de producción.

El backend requiere `DATABASE_URL` y `JWT_SECRET`; acepta además `REDIS_ADDR`, `REDIS_PASSWORD`, `JWT_ISSUER` y `HTTP_ADDR`. El secreto JWT debe tener al menos 32 bytes. Consulta el [README](../README.md) para los comandos de inicio y compilación.

## Flujo de autenticación

1. El rate limiter cuenta el intento por IP y, si puede extraerlo del JSON, por email; los contadores usan una ventana fija de 60 segundos.
2. Si se excede alguno de los límites, la API responde `429` y `Retry-After`; un error de Redis se registra y no bloquea el login.
3. El handler delega al servicio, que consulta PostgreSQL y compara la contraseña con el hash bcrypt.
4. El servicio firma un JWT de acceso de 15 minutos y crea un refresh token aleatorio de 7 días; PostgreSQL almacena solo el hash de este último.
5. El backend devuelve el access token y el rol, y envía el refresh token en una cookie segura.
6. Cuando una petición recibe `401`, Angular comparte una única llamada a `/api/auth/refresh`; el backend rota el token en la misma familia dentro de una transacción y Angular reintenta las peticiones pendientes.
7. El middleware Go valida el access token y consulta Redis. Los endpoints protegidos pueden usar `RequireRoles` para autorizar roles en el servidor.
8. Reutilizar un refresh token ya rotado revoca toda la familia en PostgreSQL y devuelve `401`.
9. Al cerrar sesión, el backend revoca la familia de refresh tokens, revoca el `jti` del access token en Redis y limpia la cookie.

Si Redis no está disponible, el rate limiter de login registra el error y deja pasar la petición; el middleware JWT de las rutas protegidas falla de forma cerrada y no la autoriza. El rol almacenado en el cliente sirve para adaptar la interfaz, no para tomar decisiones de seguridad.
