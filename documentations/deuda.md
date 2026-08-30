# Deuda viva de `wapp-platform-console`

Verificada el **2026-08-30** sobre `main` `b89c803`, leyendo el código. Cada entrada dice **dónde**,
**qué consecuencia tiene** y **cómo se cerraría**. Lo que no se comprobó va marcado
**NO VERIFICADO**.

**No hay ni un `TODO`, `FIXME` ni `HACK` en todo el repo** (`grep -rn -E 'TODO|FIXME|HACK|XXX'` →
cero). La deuda de este repo se escribe con su razón, y por eso está aquí.

Prioridad: 🔴 abre un riesgo o miente al operador · 🟡 molesta o envejece mal · ⚪ limpieza.

---

## 🔴 D-1 · La consola no autentica a nadie: la cookie de sesión no está firmada

**Dónde:** `internal/web/session.go:76` (`unverifiedParser`), `internal/web/session.go:83`
(`ParseUnverified`), `internal/web/auth_handler.go:115` (`AuthMiddleware`).

**Qué pasa:** la cookie de sesión es `base64(JSON)` **sin firmar ni cifrar**, y el JWT que lleva
dentro se lee sin verificar la firma. El middleware solo comprueba que haya un `exp` en el futuro.
⇒ **cualquiera con acceso a `:8106` puede fabricar una cookie a mano y navegar todas las pantallas
protegidas.** La prueba está en la propia suite: `internal/web/server_test.go:32` firma el token con
la clave literal `"dummy"` y la consola lo acepta.

**Por qué no es una brecha hoy:** el gate real es `:8100`, que sí valida el Bearer, el RBAC `.any`
y la pertenencia al tenant de plataforma; y esta consola escucha en **loopback**, tras túnel SSH.
Un token falso ve pantallas vacías y 401/403.

**Cómo se cerraría:** es la deuda **M-09** del code review del Plan 056 y su enunciado sigue siendo
el bueno — *decidir por escrito* si la cookie se firma con HMAC. Comprobado:
`grep -n "hmac\|HMAC\|Sign" internal/web/session.go` → **0 aciertos**. Si se decide que sí, la
firma va en `wapp-shared/web` (no aquí: la consola del cliente tiene el mismo problema y no queremos
un tercer fork). Si se decide que no, la decisión y su mitigación (loopback + no publicar) se
escriben y esta entrada se cierra como decisión, no como deuda.

---

## 🔴 D-2 · El desplegable de planes se quedó atrás y su candado no puede verlo

**Dónde:** `internal/web/templates/pages/tenant_new.html:46-50` (las cinco `<option>`),
`internal/web/catalog_test.go:33` (`planesSembrados`),
`internal/web/templates/partials/plan_label.html` (la frase «el catálogo real es basic, pro,
commerce, advisor_ai, advisor_ai_pro»).

**Qué pasa:** los tres sitios afirman que `public.plans` tiene **cinco** planes. **Son seis desde
el 2026-08-23**: la migración `0074_seed_plan_advisor_ai_local.sql` del cloud siembra
`advisor_ai_local`. Consecuencias reales:

1. **Desde esta consola no se puede dar de alta una empresa con el plan `advisor_ai_local`** — el
   plan del LLM local. Asignarlo sigue siendo trabajo de SQL.
2. El candado de `catalog_test.go` es **de un solo sentido**: comprueba `ofrecido ⊆ sembrado`,
   nunca `sembrado ⊆ ofrecido`. Un plan nuevo en el cloud **envejece en silencio**, que es
   exactamente el fallo que ese fichero decía haber cerrado.

**Cómo se cerraría:** añadir la `<option>` y la entrada en `planesSembrados` es el parche de hoy;
el arreglo es **invertir también el aserto** (fallar si el catálogo del cloud tiene un plan que la
consola no ofrece, con una lista explícita de exclusiones justificadas). El arreglo de fondo es la
deuda D-3.

---

## 🔴 D-3 · Dos catálogos duplicados a mano porque no existe endpoint que los liste

**Dónde:** `internal/web/templates/pages/tenant_new.html:35` (planes) y
`internal/web/templates/pages/access_requests.html:58` (roles). Ambas llevan el comentario
**«DEUDA CONOCIDA (acoplamiento)»** escrito por sus autores.

**Qué pasa:** las listas duplican `public.plans` y `public.iam_roles` del cloud. `tenants.plan_id`
es FK: un `value` que no exista **revienta la FK y el operador ve un 500 opaco**, porque el mapeo de
errores del cloud solo reconoce la violación de UNIQUE, no la de FK. En roles, un nombre inexistente
da 400. Ya costó una factura: en el primer commit los desplegables ofrecían identificadores
**inventados** (`standard`, `enterprise`, `admin`), y **nombrar al administrador de una empresa era
imposible desde la UI**.

**Cómo se cerraría:** un endpoint de solo lectura en `:8100` que liste planes y roles, y que la
plantilla los recorra. Mientras no exista, el `value` **debe** ser el id real y el candado de
`catalog_test.go` es lo único que lo sostiene.

---

## 🔴 D-4 · «Opera los planes» es falso: no hay forma de cambiarle el plan a una empresa

**Dónde:** ausencia. `internal/web/server.go:179-192` (las **11** rutas protegidas; las 19 del repo
son esas 11 más las 8 públicas de `:106-138` y `:170-172`) y, en el cloud,
`registerAdminRoutes`.

**Qué pasa:** el `plan_id` solo se puede fijar **en el alta** (`POST /tenants/new`). **No existe
ninguna ruta para cambiar el plan de una empresa existente**, ni en esta consola ni en el cloud.
Subir a un cliente de `basic` a `pro` sigue siendo un `UPDATE` a mano — que es justo el escenario
que el Plan 056 vino a eliminar. Cualquier documento que diga que esta consola «opera los planes»
está describiendo algo que no existe.

**Cómo se cerraría:** endpoint `PATCH /admin/tenants/{id}` (o similar) con permiso `.any` propio,
más una acción en la ficha de empresa. Requiere decidir qué pasa con las `tenant_features` ya
concedidas.

---

## 🔴 D-5 · El primer administrador no tiene procedimiento con registro

**Dónde:** ausencia en este repo. El `Makefile` no tiene ningún target de identidades.

**Qué pasa:** sin una cuenta con acceso a `wapp.platform` la consola no sirve para nada, y **no hay
comando que la cree**. El bootstrap del ecosistema está bloqueado a `localhost` **sin variable de
escape**. En la práctica se hizo **dos veces con `psql` crudo, fuera del `Makefile`, y ningún
documento conservó el comando**. Cuando se midió: `iam.api_keys` vacía y 7 usuarios con 0
asignaciones de rol — **ningún administrador en toda la base**. *«El acto más sensible del sistema
es el único sin registro.»*

**Estado hoy:** existe un runbook, pero **en el repo de documentación del ecosistema**, no aquí, y
con **dos afirmaciones caducas** (manda abrir el System Gate para `wapp.bff` cuando esta consola
exige `wapp.platform`; y habla de Neon cuando la base de UAT es un PostgreSQL 17 en Docker).
El procedimiento resumido y corregido está en [`operacion.md`](operacion.md) §4.

**Cómo se cerraría:** un comando con traza (auditoría) que cree la cuenta de staff en un ambiente
remoto, en el repo que sea su dueño. Mientras tanto, mantener §4 de `operacion.md` al día es la
única red.

---

## 🟡 D-6 · El rate-limit nunca limita por usuario

**Dónde:** `internal/web/server.go:68` (instalación del limitador) frente a
`internal/web/server.go:176` (`AuthMiddleware`).

**Qué pasa:** la clave del limitador combina el user id del contexto con la IP, pero el limitador
se instala **antes** del `AuthMiddleware`, así que cuando corre, el user id **aún no está en el
contexto**. En la práctica limita solo por IP. Con la consola tras un túnel SSH, todos los
operadores comparten IP.

**Cómo se cerraría:** un segundo limitador dentro del grupo protegido, o mover la resolución de
identidad antes. Ojo con no dejar las rutas públicas sin límite al moverlo.

---

## 🟡 D-7 · `/healthz` está detrás del rate-limit

**Dónde:** `internal/web/server.go:138`, registrada después de `server.go:68`.

**Qué pasa:** un monitor agresivo que comparta IP con tráfico real puede recibir **429** en el
health check y declarar caída una consola sana.

**Cómo se cerraría:** registrar `/healthz` antes del `router.Use(webgin.RateLimit(...))`, junto a
las rutas de CSS, o excluir esa ruta en el middleware.

---

## 🟡 D-8 · Un `:8100` caído se sirve como `200 OK`

**Dónde:** `internal/web/tenants_handler.go:33` (portada) y
`internal/web/access_requests_handler.go:34` (bandeja).

**Qué pasa:** cuando el upstream no responde, la portada se pinta con **`http.StatusOK`** y el
texto «No se pudieron cargar las empresas»; la bandeja se pinta **vacía y sin aviso**. Para una
sonda o un proxy, la consola está sana. Un operador puede leer «no hay solicitudes pendientes»
cuando lo cierto es «no se pudieron leer».

**Cómo se cerraría:** un `502` (o `503`) en la portada, y un aviso explícito en la bandeja usando el
catálogo de flash — que ya tiene `tenant_unreachable` disponible.

---

## 🟡 D-9 · Rebote mudo: la ficha de empresa que falla redirige sin decir nada

**Dónde:** `internal/web/tenants_handler.go:64`.

**Qué pasa:** si `GetTenant` falla, `ShowTenantDetail` redirige a `/` **sin ningún flash**. El
operador vuelve al listado sin saber por qué. Es la **única ruta de error del repo** que no pone
código de flash, teniendo `tenant_unreachable` ya usado 37 líneas más abajo
(`internal/web/tenants_handler.go:101`).

**Cómo se cerraría:** una línea: `c.Redirect(http.StatusSeeOther, "/?error=tenant_unreachable")`,
y un aserto en `tenants_test.go`.

---

## 🟡 D-10 · Error tragado en el refresco de sesión

**Dónde:** `internal/web/auth_handler.go:215` — `_ = h.startSession(c, res)`.

**Qué pasa:** si re-empaquetar la cookie falla **después** de un refresh correcto, el operador se
queda con el token viejo **sin una sola línea de log**, y lo verá como un cierre de sesión
inexplicable. Contrasta con el resto del fichero, que loguea meticulosamente — incluido el `switch`
401/403 de `DoLogin`, que existe precisamente porque un fallo sin log costó una tarde.

**Cómo se cerraría:** `if err := h.startSession(c, res); err != nil { slog.Error(...) }`.

---

## 🟡 D-11 · `POST /tenants/new` no hace PRG: un F5 reenvía el alta

**Dónde:** `internal/web/provisioning_handler.go:102` (éxito con `200` **sobre el POST**) y
`:64` / `:87` (los dos rechazos, `400` repintando).

**Qué pasa:** es la única mutación del repo sin POST-Redirect-GET. Un F5 en la pantalla «Empresa
creada» **reenvía el alta**. Es exactamente la forma que la corrección **M-10** eliminó en la ruta
de al lado (el código de enrolamiento).

**NO VERIFICADO:** qué ocurre al reenviar — si el cloud rechaza el slug duplicado con un 409 o crea
una segunda empresa. No se leyó la unicidad del slug en el cloud.

**Cómo se cerraría:** 303 al detalle de la empresa recién creada, con `?success=`, y mover las dos
salidas de la pantalla «Empresa creada» allí.

---

## 🟡 D-12 · Sin paginación: la empresa 51 es invisible

**Dónde:** `internal/web/tenants_handler.go:30` (`limit=50`, literal) y
`internal/web/access_requests_handler.go:37` (`limit=100`, literal).

**Qué pasa:** ni `offset` ni UI de siguiente página ni buscador. **A partir de la empresa 51 la
consola es ciega**, y no se puede seleccionar la empresa 101 al aprobar una solicitud.
`ListTenants` **sí acepta `offset`** (`internal/adminclient/tenants.go`), pero nadie se lo pasa.
Es la deuda **M-14** del code review del Plan 056, todavía abierta.

**Cómo se cerraría:** buscador por slug o nombre (mejor que paginar) o paginación con `offset`.
🔴 Si haces un test de paginación, **verifícalo por mutación**: uno con 12 filas seguía verde tras
quitarle el `ORDER BY`.

---

## 🟡 D-13 · La barrera humana del corte enseña la respuesta

**Dónde:** `internal/web/templates/pages/tenant_detail.html:118` (el slug en un `<code>`) y `:125`
(el mismo slug como `placeholder` del input).

**Qué pasa:** el handler razona bien —el slug esperado se resuelve **en el servidor**, nunca viaja
en un campo oculto (INV-PC4)— pero la plantilla imprime el slug exacto justo encima del campo **y
además lo pone de `placeholder`**. Lo que queda es fricción («copia esto ahí»), no confirmación
fuera de banda. La barrera sigue impidiendo el clic accidental; no impide el copiar-pegar sin
mirar.

**Cómo se cerraría:** decidir qué se quiere. Si basta la fricción, escribirlo así y quitar el
`placeholder` (que es el que más invita a copiar). Si se quiere confirmación de verdad, el slug
tiene que venir de otro sitio: la ficha del cliente, un segundo factor, o un segundo par de ojos.

---

## 🟡 D-14 · El TTL del código de enrolamiento está cableado a 24 h

**Dónde:** `internal/web/provisioning_handler.go:129` — el literal `86400`.

**Qué pasa:** el operador no puede elegir el TTL aunque **el servidor acepta de 60 s a 30 días**.
Un código de enrolamiento vive 24 h por defecto y no hay forma de emitir uno corto para una llamada
telefónica.

**Cómo se cerraría:** un campo en el formulario y validación del rango en el handler.
🔴 Al tocarlo, recuerda que la clave JSON es **`ttl`** y que hay un test que lo blinda.

---

## 🟡 D-15 · Dos variables que el código lee no están en `.env.example`

**Dónde:** `internal/config/config.go:70-71` — `WAPP_CONSOLE_RATE_TTL_SECS` y
`WAPP_CONSOLE_RATE_PURGE_SECS`. `.env.example`, que por lo demás documenta hasta los alias legados,
no las menciona.

**Qué pasa:** quien configure el rate-limit desde `.env.example` no sabe que existen. Es menor
porque **el `.env` no lo lee nadie** de todos modos (ver D-16).

**Cómo se cerraría:** dos líneas en `.env.example`.

---

## 🟡 D-16 · El `.env` es decorativo: no lo carga nadie

**Dónde:** `internal/config/config.go:48` (el lector solo consulta el entorno del proceso) frente a
`.env.example` y `.gitignore:16-17`, que reservan `.env`.

**Qué pasa:** copiar `.env.example` a `.env` y hacer `make run` arranca con **todos los defaults**,
**en silencio**. No hay `godotenv` en `go.sum` (`grep -c godotenv go.sum` → 0). Combinado con D-17
(nada es obligatorio), el resultado es una consola que parece configurada y no lo está.

**Cómo se cerraría:** o se carga el fichero (una dependencia más), o `.env.example` deja de
llamarse así y pasa a ser un fragmento de `EnvironmentFile` documentado como tal.

---

## 🟡 D-17 · Nada de la configuración es obligatorio

**Dónde:** `internal/config/config.go:47-80` — las 24 lecturas, **todas con default**.

**Qué pasa:** un despliegue mal configurado **arranca «bien»** apuntando a `127.0.0.1` aunque en
producción no haya nadie ahí, y el único síntoma es una portada que dice «No se pudieron cargar las
empresas» con status 200 (D-8). El único fallo duro del arranque es un `TrustedProxies` inválido.
Agravante: `internal/config` **no tiene ni un test**, así que los tres alias legados no los prueba
nada.

**Cómo se cerraría:** exigir `WAPP_ADMIN_API_BASE` e `WAPP_IDENTITY_URL` cuando el ambiente no es
`local`, y fallar en el arranque si faltan. Más un test de `Load()` que cubra los alias.

---

## 🟡 D-18 · Cada `c.HTML` copia las mismas claves a mano

**Dónde:** los 10 `c.HTML` de producción: `internal/web/auth_handler.go:225`,
`internal/web/access_requests_handler.go:43`, `internal/web/provisioning_handler.go:46,64,87,102,187`
y `internal/web/tenants_handler.go:33,45,74`.

**Qué pasa:** cada render repite `CSRFToken`, `Nonce`, `IsAuthenticated`, `CurrentPath`… y **no hay
helper**. Olvidar `Nonce` deja la página sin estilo permitido; olvidar `CSRFToken` **rompe el
formulario en runtime, sin que compile mal**. Hoy los 10 están completos (10 `c.HTML` ↔ 10 `Nonce`
↔ 10 `CSRFToken`) y `security_test.go` cubre las 7 páginas, pero **nada impide que la 11.ª nazca
coja**.

**Cómo se cerraría:** un helper que reciba lo específico y rellene lo común; o, más barato, un test
sobre el AST que exija esas claves en todo `c.HTML` del paquete. La segunda opción sigue la
doctrina del ecosistema: si N sitios repiten el mismo invariante, se testea la **regla**, no N
conductas.

---

## 🟡 D-19 · Asimetría de escapado entre la URL upstream y el `Location` del redirect

**Dónde:** `internal/adminclient/tenants.go` (usa `url.PathEscape`, con cuatro tests) frente a
`internal/web/tenants_handler.go:101,109,119,123,138,142`, que construyen
`"/tenants/" + id + "?error=..."` **sin escapar**.

**Qué pasa:** un `id` que contenga un `%3F` decodificado a `?` mete texto del atacante en el query
string que el propio handler lee luego con `c.Query`. **El impacto máximo que se ve es falsear el
mensaje de flash** —mostrar «Servicio cortado correctamente» sin que se cortara nada—, porque el
catálogo cerrado de `flash.go` impide reflejar texto arbitrario (INV-PC6).

**NO VERIFICADO:** la explotación concreta. No se ejecutó la petición.

**Cómo se cerraría:** `url.PathEscape(id)` también al construir el `Location`, y un test que meta
un id con caracteres reservados.

---

## ⚪ D-20 · Código muerto verificado: la función de plantilla `hasPrefix`

**Dónde:** `internal/web/server.go:73`.

**Qué pasa:** `hasPrefix` se registra en el `FuncMap` y **no la usa ninguna plantilla**:
`grep -rc 'hasPrefix' internal/web/templates/` → **0**. Su vecina `has` sí se usa, en
`internal/web/templates/pages/access_requests.html:86-87`.

**Cómo se cerraría:** borrar la línea. El linter `unused` no la ve porque `strings.HasPrefix` sí
existe: lo muerto es el registro, no la función.

---

## ⚪ D-21 · Supresión de linter viva

**Dónde:** `internal/web/server.go:91` — `// #nosec G203` sobre un `template.HTML(buf.String())`
dentro de la función `yield`.

**Qué pasa:** es **correcto** (el contenido lo produce el propio motor de plantillas, ya escapado),
pero es una supresión que hay que volver a justificar si alguien cambia lo que entra en `yield`.
Se anota para que no envejezca en silencio.

**Cómo se cerraría:** nada que hacer hoy. Si `yield` pasara a recibir contenido de fuera, la
supresión deja de valer.

---

## ⚪ D-22 · `v0.1.0` va 4 commits por detrás del HEAD

**Dónde:** los tags del repo. `v0.1.0` apunta a `5e447dd`; el HEAD es `b89c803`.

**Qué pasa:** el único tag publicado no describe lo que corre. El binario de UAT no se instaló
desde el tag: lleva empotrada la pseudo-versión de `b89c803`. Nadie puede pedir «la v0.1.0» y
obtener lo que está desplegado.

**Cómo se cerraría:** cortar `v0.1.1` desde `main` (ver [`operacion.md`](operacion.md) §3).

---

## Cosas que se buscaron y NO se encontraron

Se dicen porque su ausencia es información:

- **Credenciales o secretos en el código de producción**: ninguno. El único literal parecido es
  `c.PostForm("password")`, que es el nombre del campo del formulario.
- **Concurrencia dudosa**: el único estado compartido es el `RefreshGroup` y el rate-limiter, ambos
  del módulo compartido. `go test -race` pasa.
- **Funciones gigantes**: la mayor es `NewRouterWithLimiter` (156 líneas), y es un registro de
  rutas lineal.
- **Errores ignorados con `_`** más allá de D-10 y los `drainClose` / decode acotado deliberados de
  `internal/adminclient/transport.go`.
- **Fugas de PII al log**: el correo no se registra en el login fallido, y el código de enrolamiento
  no se registra nunca.
