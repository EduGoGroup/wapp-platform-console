# Contratos de `wapp-platform-console`

Todo lo que otros consumen de esta pieza y todo lo que ella consume. Verificado el **2026-08-30**
sobre `main` `b89c803`.

**De dónde salen las listas y con qué regla se contaron** — dilo antes de creerte un número:

| Lista | Fuente | Regla de conteo |
|---|---|---|
| Rutas que sirve | `internal/web/server.go`, función `NewRouterWithLimiter` — **único** sitio del repo donde se registra una ruta | `grep -c 'router.GET\|router.POST\|protected.GET\|protected.POST' internal/web/server.go` → **19** = **8 públicas** (`:106-138`, las 4 de CSS y `/healthz`; `:170-172`, login GET/POST y logout) + **11 protegidas** (`:179-192`) |
| Rutas que consume | los cuatro ficheros de `internal/adminclient/` | una llamada = un `HTTPClient.Do` con su método y su path → **10** |
| Variables de entorno | `internal/config/config.go`, función `Load()` | un literal `Get*("CLAVE")` = una variable, **con el prefijo `WAPP_` ya aplicado** → **24** (incluye 3 alias legados) |
| Ficheros que escribe | `grep -rn "os.Create\|os.WriteFile\|os.OpenFile"` | → **0** |
| Comandos CLI / gRPC | `grep -rn "flag\.\|grpc\." --include='*.go'` | → **0** |

---

## 1 · Las 19 rutas HTTP que SIRVE

Las consume el navegador del operador. Cada acción es un `<form method="POST">` clásico: **no hay
JavaScript ni API JSON**, salvo `/healthz`.

### Públicas — sin sesión

Las cinco primeras se registran **antes** del middleware CSRF, así que quedan fuera de él; sí pasan
por rate-limit, CORS y cabeceras de seguridad.

| Método | Ruta | Qué devuelve |
|---|---|---|
| GET | `/static/css/app.css` | el CSS propio embebido, `Cache-Control: public, max-age=3600` |
| GET | `/static/css/theme-platform.css` | hoja de `wapp-shared/ui` vía `ui.GetCSS`; 404 si el módulo no la trae |
| GET | `/static/css/wapp-tokens.css` | ídem |
| GET | `/static/css/wapp-components.css` | ídem |
| GET | `/healthz` | `200 {"status":"healthy","time":"<RFC3339 UTC>"}`. 🔴 queda **detrás del rate-limit** |
| GET | `/login` | la pantalla de entrada (303 a `/` si ya hay sesión válida) |
| POST | `/login` | 303 a `/` si entra; 400 si falta un campo; **401 repintando** si no |
| POST | `/logout` | 303 a `/login`. Borra la cookie **siempre**, aunque falle la revocación remota |

### Protegidas — grupo con `AuthMiddleware` + `RequestDeadline`

**Dominio empresas**

| Método | Ruta | Qué hace |
|---|---|---|
| GET | `/` | portada: listado de empresas, `limit=50` **fijo**, sin paginación |
| GET | `/tenants/new` | formulario de alta |
| POST | `/tenants/new` | 🔴 **no redirige**: 400 repintando en rechazo, 200 con «Empresa creada» **sobre el POST** en éxito |
| GET | `/tenants/:id` | ficha: plan, features, instalaciones (Edge Fleet) y las tres mutaciones |
| POST | `/tenants/:id/revoke` | **kill-switch comercial**. Exige teclear el slug. 303 |
| POST | `/tenants/:id/restore` | restaura el servicio. 303 |

**Dominio enrolamiento**

| Método | Ruta | Qué hace |
|---|---|---|
| POST | `/tenants/:id/enrollment-codes` | emite el código, lo deja en la cookie efímera y **303** |
| GET | `/tenants/:id/enrollment-code` | consume la cookie de un solo uso y muestra el código |

**Dominio bandeja de acceso**

| Método | Ruta | Qué hace |
|---|---|---|
| GET | `/access-requests` | solicitudes en estado `pending`; el desplegable de empresas usa `limit=100` **fijo** |
| POST | `/access-requests/:id/approve` | aprueba con empresa + rol + sistemas. 303 |
| POST | `/access-requests/:id/reject` | rechaza con motivo obligatorio. 303 |

### Campos de formulario (el contrato con el navegador)

| Ruta | Campos |
|---|---|
| `POST /login` | `email`, `password` |
| `POST /tenants/new` | `slug`, `display_name`, `plan_id` |
| `POST /tenants/:id/revoke` | `slug_confirm`, `reason` |
| `POST /tenants/:id/restore` | `reason` |
| `POST /access-requests/:id/approve` | `tenant_id`, `role`, `systems[]` |
| `POST /access-requests/:id/reject` | `reason` |

**`csrf_token` va en todos los POST.** Un motivo ausente en corte o restauración no falla: se
sustituye por un texto por defecto («Corte administrativo desde consola de plataforma» /
«Restauración administrativa…»). En el **rechazo** de una solicitud, en cambio, el motivo **sí** es
obligatorio.

### Códigos de flash — vocabulario estable de la URL

Lo que la consola pone en `?error=` / `?success=` al redirigir. Fuente: `internal/web/flash.go`.
Un código desconocido cae al genérico y **nunca** se refleja texto crudo del upstream (INV-PC6).

**Errores:** `missing_fields`, `missing_systems`, `approve_failed`, `approve_partial`,
`approve_partial_skipped`, `missing_reason`, `reject_failed`, `tenant_unreachable`,
`slug_mismatch`, `revoke_failed`, `restore_failed`, `code_failed`, `code_lost`.
**Éxitos:** `approved`, `rejected`, `revoked`, `restored`.

### Cookies que emite

| Cookie | Vida | Contenido | Notas |
|---|---|---|---|
| `wapp_platform_session` | 86 400 s (24 h) | `base64(JSON{access_token, refresh_token, expires_at})` | HttpOnly siempre. 🔴 **sin firmar ni cifrar** — ver INV-PC1 |
| `wapp_platform_csrf` | 24 h | token CSRF | HttpOnly y SameSite=Lax fijados por el módulo compartido |
| `wapp_platform_enrollment_code` | **60 s** | el código en tránsito POST→GET | `Path` = `/tenants/{id}/enrollment-code`; de un solo uso |

`Secure` y `SameSite` de la de sesión y la efímera salen de la configuración; el `SameSite=Lax` y el
`HttpOnly` de la de CSRF los fija el módulo **siempre**, para que el fail-safe no se degrade.

---

## 2 · Las 10 rutas que CONSUME del listener admin `:8100`

Todas con `Authorization: Bearer <access token de la sesión>` y `Accept: application/json`. Las 10
verificadas contra `wapp-cloud-platform` (`registerAdminRoutes` en `internal/bootstrap/bootstrap.go`):
**ninguna huérfana, ninguna inventada**.

| Método | Ruta upstream | Cuerpo | Permiso que exige el cloud |
|---|---|---|---|
| GET | `/admin/tenants?limit=&offset=` | — | `tenants.read.any` |
| GET | `/admin/tenants/{id}` | — | `tenants.read.any` |
| POST | `/admin/tenants` | `{slug, display_name, plan_id}` | `tenants.create.any` |
| POST | `/admin/tenants/revoke` | `{tenant_id, reason}` | `tenants.revoke.any` |
| POST | `/admin/tenants/restore` | `{tenant_id, reason}` | `tenants.restore.any` |
| POST | `/admin/tenants/{id}/enrollment-codes` | `{"ttl": 86400}` | `enrollment.issue.any` |
| GET | `/admin/tenants/{id}/installations` | — | `fleet.read.any` |
| GET | `/admin/access-requests?status=` | — | `users.provision.any` |
| POST | `/admin/access-requests/{id}/approve` | empresa, rol y sistemas | `users.provision.any` |
| POST | `/admin/access-requests/{id}/reject` | motivo | `users.provision.any` |

🔑 **La clave del TTL es `ttl`, no `ttl_seconds`.** El servidor la declara así. `encoding/json`
ignora claves desconocidas **sin error**, así que un desajuste haría que el TTL nunca llegara.
Lo blinda `TestTenants_IssueEnrollmentCode_SendsTTLKeyNotTTLSeconds`.

🔑 **En `revoke`/`restore` el tenant objetivo viaja en el CUERPO.** Eso es la excepción
administrativa a INV-8 del ADR-0039, autorizada — ver la sección 2 de
[`constitucion.md`](constitucion.md). El `{id}` del path se escapa con `url.PathEscape` en las
cuatro rutas que lo llevan, con test por cada una.

**Formas que espera de vuelta** (`internal/adminclient/`): `TenantSummary` (`id`, `slug`,
`display_name`, `plan_id`, `revoked_at`), `TenantDetail` (además `created_at`,
`installations_count`, `features[]`), `TenantCreateResult`, `EnrollmentCodeResult`
(`code`, `expires_at`), `InstallationItem` (`edge_id`, `sessions`, `last_seen_at`,
`lease_revoked`) y `AccessRequestItem` (`id`, `user_id`, `email`, `origin`, `created_at`,
`systems[]`, `systems_known`).

Todo cuerpo se lee acotado: 1 MiB en éxito, 4 KiB en el camino de rechazo.

---

## 3 · Lo que consume de identity (`:8200`) y de la API pública (`:8103`)

Indirecto, a través del módulo compartido `wapp-shared/iam`, configurado con
`System: "wapp.platform"`:

| Destino | Ruta | Cuándo |
|---|---|---|
| identity `:8200` | `POST /api/v1/auth/login` | al enviar el formulario de login |
| identity `:8200` | `POST /api/v1/auth/refresh` | refresco proactivo del token, serializado por `RefreshGroup` |
| identity `:8200` | `POST /api/v1/auth/logout` | al cerrar sesión |
| API pública `:8103` | `POST /api/v1/auth/exchange` | canje Identity Token → Context Token, **solo** eso |

El canje del cloud acepta hoy `wapp.bff`, `wapp.edge` y **`wapp.platform`**, en ese orden.

---

## 4 · Variables de entorno

**Nombres efectivos en la máquina.** El código escribe el literal sin prefijo y el loader compone
`WAPP_` (`sharedconfig.New(sharedconfig.WithEnvPrefix("WAPP_"))` en `internal/config/config.go`):
el `CONSOLE_ENV` del código es **`WAPP_CONSOLE_ENV`** en el entorno.

🔴 **Ninguna es obligatoria: todas tienen default.** Un despliegue mal configurado arranca igual.
🔴 **El fichero `.env` no lo lee nadie**: no hay `godotenv`. `.env.example` documenta, no carga.

| Variable (nombre efectivo) | Default | Nota |
|---|---|---|
| `WAPP_CONSOLE_ENV` | `local` | alias legado: `WAPP_PLATFORM_CONSOLE_ENV`. Distinto de `local` ⇒ cookie `Secure` y HSTS por defecto, y log en JSON |
| `WAPP_PLATFORM_CONSOLE_HTTP_ADDR` | `127.0.0.1:8106` | alias legado: `WAPP_CONSOLE_HTTP_ADDR` |
| `WAPP_ADMIN_API_BASE` | `http://127.0.0.1:8100` | el listener admin. Sin alias |
| `WAPP_PUBLIC_API_BASE` | `http://127.0.0.1:8103` | solo para el canje del login |
| `WAPP_IDENTITY_URL` | `http://127.0.0.1:8200` | emisor de Identity Tokens |
| `WAPP_CONSOLE_COOKIE_SECURE` | sigue a `ENV != local` | |
| `WAPP_CONSOLE_COOKIE_SAMESITE` | `lax` | `none` obliga `Secure=true` |
| `WAPP_CONSOLE_ALLOWED_ORIGINS` | `""` (same-origin) | CSV de orígenes completos. **Nunca `*`** |
| `WAPP_PLATFORM_CONSOLE_TRUSTED_PROXIES` | `""` | alias legado: `WAPP_CONSOLE_TRUSTED_PROXIES`. 🔴 un valor con formato inválido hace **`panic` en el arranque** |
| `WAPP_CONSOLE_HSTS_ENABLED` | sigue a `COOKIE_SECURE` | |
| `WAPP_CONSOLE_RATE_ENABLED` | `true` | |
| `WAPP_CONSOLE_RATE_RPS` | `5` | |
| `WAPP_CONSOLE_RATE_BURST` | `10` | |
| `WAPP_CONSOLE_RATE_TTL_SECS` | `300` | 🔴 **no documentada en `.env.example`** |
| `WAPP_CONSOLE_RATE_PURGE_SECS` | `60` | 🔴 **no documentada en `.env.example`** |
| `WAPP_CONSOLE_READ_HEADER_TIMEOUT_SECS` | `5` | anti-slowloris |
| `WAPP_CONSOLE_READ_TIMEOUT_SECS` | `15` | |
| `WAPP_CONSOLE_WRITE_TIMEOUT_SECS` | `30` | |
| `WAPP_CONSOLE_IDLE_TIMEOUT_SECS` | `60` | |
| `WAPP_CONSOLE_SHUTDOWN_TIMEOUT_SECS` | `10` | plazo del apagado ordenado |
| `WAPP_CONSOLE_UPSTREAM_TIMEOUT_SECS` | `20` | fija el `http.Client.Timeout` de `adminclient` **y** el `RequestDeadline` del grupo protegido |

**Los 3 alias legados** (`WAPP_PLATFORM_CONSOLE_ENV`, `WAPP_CONSOLE_HTTP_ADDR`,
`WAPP_CONSOLE_TRUSTED_PROXIES`) se prueban **después** del nombre principal. Define uno de los dos,
no ambos. Ninguno de los tres tiene test: `internal/config` no tiene ni un fichero de test.

### 🔴 16 nombres los COMPARTE con `wapp-client-console`, y ambas corren en la misma máquina

Las dos consolas componen el mismo prefijo `WAPP_`, así que **toda la familia `WAPP_CONSOLE_*` es
común**:

```
WAPP_CONSOLE_ALLOWED_ORIGINS   WAPP_CONSOLE_COOKIE_SAMESITE  WAPP_CONSOLE_COOKIE_SECURE
WAPP_CONSOLE_ENV               WAPP_CONSOLE_HSTS_ENABLED     WAPP_CONSOLE_IDLE_TIMEOUT_SECS
WAPP_CONSOLE_RATE_BURST        WAPP_CONSOLE_RATE_ENABLED     WAPP_CONSOLE_RATE_PURGE_SECS
WAPP_CONSOLE_RATE_RPS          WAPP_CONSOLE_RATE_TTL_SECS    WAPP_CONSOLE_READ_HEADER_TIMEOUT_SECS
WAPP_CONSOLE_READ_TIMEOUT_SECS WAPP_CONSOLE_SHUTDOWN_TIMEOUT_SECS
WAPP_CONSOLE_UPSTREAM_TIMEOUT_SECS  WAPP_CONSOLE_WRITE_TIMEOUT_SECS
```

En UAT conviven en `127.0.0.1:8106` (esta) y `127.0.0.1:8107` (la del cliente). Un
`WAPP_CONSOLE_ENV` o un `WAPP_CONSOLE_COOKIE_SECURE` exportado en el entorno del host **se aplica a
las dos a la vez**. Lo que resuelve la ambigüedad hoy son **ficheros de entorno separados** por
unidad systemd. Lo que **sí** está desambiguado por nombre es la dirección de escucha
(`WAPP_PLATFORM_CONSOLE_HTTP_ADDR` frente a `WAPP_CLIENT_CONSOLE_HTTP_ADDR`) y los proxies de
confianza. `WAPP_IDENTITY_URL` y `WAPP_PUBLIC_API_BASE` también coinciden, pero ahí es legítimo:
son el mismo upstream. **`WAPP_ADMIN_API_BASE` es exclusiva de esta consola** y así debe seguir: la
del cliente no habla con `:8100`.

---

## 5 · Ficheros y persistencia

- **Ficheros que escribe: ninguno.** Toda la salida va a **stdout** por `slog`: texto si el
  ambiente es `local`, **JSON** en cualquier otro caso. No hay un solo `os.Create`/`os.WriteFile`.
- **Ficheros que lee en ejecución: ninguno.** Plantillas y CSS propio van **embebidos en el
  binario** con `//go:embed`; las tres hojas compartidas salen del módulo `wapp-shared/ui`, también
  compilado dentro.
- **Base de datos: ninguna.** Sin migraciones, sin DSN, sin versión de esquema.

### Qué se registra en ejecución, y con qué regla

La regla es **causa sí, PII no**. En concreto:

- **Login rechazado**: se registra la **causa** distinguiendo 403 del System Gate (falta la fila en
  `iam.user_systems` para `wapp.platform`) de 401 de credenciales, y **nunca el correo**. La
  distinción se oculta en pantalla a propósito y solo existe en el log — hasta el 2026-08-28 solo
  se cumplía para una de las dos ramas, y hubo que deducir la causa por la **ausencia** de una
  línea. ✅ Lo vigila `TestAuth_ElLogDISTINGUE401De403`.
- **Código de enrolamiento**: **nunca** se registra. Cuando el empaquetado falla, el log dice que
  se perdió, sin el código.
- **Mutaciones fallidas**: se registra el `id` del tenant o de la solicitud y el error del upstream.
- **Cada petición**: la registra `SlogLogger` del módulo compartido, con método, ruta y estado.
- **Arranque**: dirección de escucha, `admin_api`, `public_api` y ambiente.

### Tablas que mueve a distancia

Esta consola no las toca; las mueven los handlers de `:8100` que llama. Saberlo importa para leer
la base cuando algo va mal: `public.tenants` (incluida `revoked_at`, el kill-switch comercial),
`public.tenant_features`, `public.plans` (catálogo, solo lectura vía el `plan_id` del alta),
`public.tenant_members`, `public.iam_user_roles` / `public.iam_roles` / `public.iam_role_grants`,
`public.access_requests` e `iam.user_systems` (esta última en **identity-core**, otra base).

---

## 6 · Comandos CLI y gRPC

**No hay.** El binario no acepta flags (`main.go` no usa `flag`), no expone gRPC y no declara
protos. Toda la configuración es por entorno y toda la operación, por navegador.
