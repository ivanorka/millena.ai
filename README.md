# Millena AI

Millena AI je lokalno pokretljiv full-stack prototip za suradnju tima i AI
asistenta na strategiji, sadržaju, kalendaru, publici i kanalima. Frontend nema
build korak, Go + Gin izlaže `/api/v1`, a PostgreSQL je izvor istine za
normalizirane domenske zapise i preostali verzionirani UI snapshot.

Sučelje je hrvatsko/englesko, s hrvatskim kao zadanim jezikom. Planirana domena
proizvoda je `millena.ai`.

## Što radi lokalno

- registracija, prijava, odjava, hashirane zaporke i server-side sesije;
- tenant izolacija, članovi tima, uloge, projektni paketi i feature ovlasti;
- MPR Grupa kao razvojni `owner` tenant s aktivnim `unlimited` paketom;
- pregled projekta i setup profila, opisa tvrtke, ritma, jezika, vremenske zone
  i web forme;
- sadržaj, kanalne/jezične varijante, strategija iz ručnog unosa ili datoteke,
  kalendar i povezano zakazivanje;
- projektne datoteke do 10 MiB, metadata/SHA-256, preuzimanje, storage quota i
  tenant izolirane veze s chat porukama ili social sandbox objavama;
- ugrađeni deterministički AI za generiranje i doradu bez računa ili API ključa,
  uz opcionalni lokalni Ollama model;
- trajni razgovori s Millena asistentom, sažetak projekta, pregled kalendara,
  izrada skice sadržaja, kontekst iz tekstualnih privitaka i
  uključivanje/isključivanje postojećeg pravila;
- automation CRUD i ručno pokretanje pravila s konkretnim učinkom u sadržaju ili
  kalendaru;
- liste publike, kontaktni CRUD, statistika, pretraga i CSV import;
- lokalne channel/social sandbox veze, test stanja, odspajanje i audit trag;
- newsletter test/schedule zapisi i povijest dostava u PostgreSQL-u;
- atomska blog–newsletter veza koja pri spremanju, premještanju ili brisanju
  zajedno održava cilj kampanje i njezine blokove;
- lokalni due workeri za izravne social sandbox objave te zakazane automation
  učinke, content varijante/publication jobove i newsletter sandbox dostave.

## Granica vanjskih integracija

Sandbox objava i newsletter dostava ne napuštaju lokalnu aplikaciju. `connected`
u sandbox načinu znači da je lokalna konfiguracija valjana, a reference oblika
`sandbox://...` nisu potvrda objave na stvarnoj mreži ili slanja emaila. Due
worker izričito odbija non-sandbox provider način dok pravi adapter ne postoji.

Za stvarni LinkedIn, Meta/Instagram/Facebook, Telegram, WhatsApp, web CMS,
newsletter ili custom API još su potrebni provider developer račun/aplikacija,
OAuth ili API vjerodajnice, enkriptirana pohrana tokena, callback/webhook rute i
provider adapter koji stvarno poziva vanjski API. Trenutačni API namjerno ne
prima niti sprema lozinke za društvene mreže.

Projektni asseti lokalno spremaju binarni sadržaj u PostgreSQL (`BYTEA`), do
10 MiB po datoteci. To je funkcionalna razvojna pohrana, ne produkcijski object
storage, antivirusni pipeline ni vizualni AI. Tekst se lokalno izdvaja iz
podržanih dokumenata i može ući u kontekst asistenta; slika ili video bez
tekstualnog sloja ostaju dostupni za preuzimanje, ali built-in engine ih ne
analizira vizualno.

Assistant file kontrola učitava asset, prikazuje uklonjive chipove i šalje
`attachmentIds` uz poruku. Social studio učitava i prikazuje image/video assete
te šalje `assetIds` pri sandbox objavi, a Blog editor sprema `content_media`
slike i njihove ID-jeve u metadata sadržaja. Content create/update pritom
provjerava da `assetIds` i `coverAssetId` pripadaju istom projektu i namjeni
`content_media`, a normalizirane veze sprema u `content_item_assets`.
Brisanje povezanog content asseta u istoj transakciji uklanja njegov ID iz
`assetIds`/`coverAssetId`, pa kompatibilna metadata ne ostaje visjeti nakon FK
cascadea.

Generički `channel-connections` endpoint za API/webhook način prima credential
samo radi provjere konfiguracije, odmah ga pretvara u SHA-256 fingerprint i u
bazu ne zapisuje izvornu vrijednost. Fingerprint nije token vault i ne može se
koristiti za vanjski poziv; produkcijski adapter trebat će zasebnu enkriptiranu
pohranu tajne.

## Glavne datoteke

- `index.html`, `login.html`, `app.html` — javna stranica, prijava i aplikacija;
- `site.css`, `site.js`, `styles.css`, `script.js`, `app-api.js` — prikaz,
  navigacija i API UI flowovi;
- `cmd/api/` — ulazna točka HTTP procesa i lokalnog sandbox workera;
- `internal/auth`, `projects`, `admin` — sesije, tenant granica, tim i paketi;
- `internal/content`, `calendar`, `assistant` — strategija, sadržaj, varijante,
  raspored i lokalni asistent;
- `internal/assets` — tenant-scoped upload, metadata, download, quota i veze
  priloga;
- `internal/workspace`, `audience`, `social`, `actions` — profil, dashboard,
  automatizacije, integracije, publika, sandbox objave i audit;
- `migrations/` — verzionirana PostgreSQL shema;
- `docs/PROJECT_PLAN_AND_ARCHITECTURE.md` — detaljna arhitektura, matrica prava i
  izvedbeni plan.

## API pregled

Javne rute:

- `GET /api/v1/health`, `GET /api/v1/ready`
- `POST /api/v1/auth/register`, `POST /api/v1/auth/login`

Prijavljeni korisnik i projekt:

- `GET /api/v1/auth/me`, `POST /api/v1/auth/logout`
- `GET/POST /api/v1/projects`
- `GET /api/v1/projects/:projectID`
- `GET/PUT /api/v1/projects/:projectID/state`
- `GET/PUT /api/v1/projects/:projectID/profile`
- `GET /api/v1/projects/:projectID/dashboard`

Projektne datoteke:

- `GET/POST /api/v1/projects/:projectID/assets`
- `GET/PUT/DELETE /api/v1/projects/:projectID/assets/:assetID`
- `GET /api/v1/projects/:projectID/assets/:assetID/download`

Upload koristi `multipart/form-data` polja `file` i `purpose`. Podržane namjene
su `assistant_attachment`, `social_media` i `content_media`; lista prihvaća
filter `?purpose=`. Social asset mora imati `image/*` ili `video/*` MIME tip.
Assistant poruka prima najviše pet `attachmentIds`, a social post najviše deset
`assetIds`; svi ID-jevi moraju pripadati istom projektu i odgovarajućoj namjeni.

Sadržaj, strategija i kalendar:

- `GET /api/v1/projects/:projectID/content`
- `GET/POST /api/v1/projects/:projectID/content/items`
- `GET/PUT/DELETE /api/v1/projects/:projectID/content/items/:itemID`
- `GET/PUT /api/v1/projects/:projectID/content/items/:itemID/variants`
- `DELETE /api/v1/projects/:projectID/content/items/:itemID/variants/:variantID`
- `GET/PUT /api/v1/projects/:projectID/strategy`
- `POST /api/v1/projects/:projectID/strategy/file`
- `GET /api/v1/projects/:projectID/content/ai/status`
- `POST /api/v1/projects/:projectID/content/ai`
- `GET /api/v1/projects/:projectID/calendar`
- `GET/PUT/DELETE /api/v1/projects/:projectID/calendar/items/:itemID`
- `POST /api/v1/projects/:projectID/calendar/items`

`content/items` create/update zadržava kompatibilna metadata polja `assetIds`
i `coverAssetId`, ali prihvaća samo kanonske UUID-jeve postojećih
`content_media` asseta iz istog projekta. Cijeli skup veza zamjenjuje se
transakcijski; nevažeći, cross-tenant ili pogrešno namijenjen asset vraća
`422 invalid_asset_references` bez djelomične izmjene sadržaja.
Master zapis, asset linkovi i automatske default-locale varijante spremaju se
atomarno. Uklanjanje kanala briše samo varijantu s
`metadata.syncedFromItem=true`; ručno uređena varijanta ostaje sačuvana.
Povezani kalendarski entry ne dopušta promjenu kanala mimo svoje varijante, a
reschedule ponovno provjerava entitlement bez dvostrukog trošenja kvote.
Zakazani newsletter koristi jedan recipient-aware delivery queue povezan s
newsletter varijantom i kalendarom; ne stvara paralelni publication job.
Content, kalendar, newsletter worker i publication worker koriste isti reducer
master statusa i najranijeg termina.
Blog se može povezati s točno određenom newsletter kampanjom. Repository u istoj
transakciji validira tenant i vrstu cilja, dodaje ili uklanja blok te pri
brisanju bilo koje strane čisti preostalu vezu. Specijalizirani editori pritom
zadržavaju metadata ključeve koje sami ne uređuju.
Automation `reviewPolicy` utječe i na ručno i na zakazano izvođenje: obvezni
review stvara `in_review` sadržaj/calendar suggestion, conditional ostaje
`draft`, a automatic sadržaj kreće kao `approved` bez automatske objave.
Oba načina izvođenja koriste isti transakcijski engine. `bot_event` može stvoriti
paket social/blog/newsletter zapisa s kanalnim varijantama, `calendar_gap`
provjerava stvarne termine prije izrade povezanog content/variant/calendar
skupa, a lokalni sat, cadence i gap dolaze iz pravila ili profila projekta.
Za isti projekt i kanal gap-runovi se serijaliziraju PostgreSQL advisory lockom,
pa dva paralelna workera ne mogu stvoriti duplikat. Eksplicitni ciljni kanali se
poštuju; ulazni Telegram/WhatsApp kanal bot pravila ne koristi se kao izlaz osim
ako je Telegram izričito naveden u konfiguraciji ciljeva.
`factCheck` i `respectForbiddenTopics` nasljeđuju se iz master pravila kada nisu
zadani lokalno; obavezna provjera ili podudaranje sa zabranjenim temama iz
strategije sprema se u metadata/audit i prisiljava ljudski pregled.
Matcher koristi granice riječi i fraza, uključujući hrvatska slova, kako kratka
zabranjena riječ ne bi slučajno blokirala nepovezanu dulju riječ.

Raspored namjerno nije potpuna RRULE implementacija. Prihvaća `gap:Nd` (1–365),
`FREQ=DAILY` s opcionalnim satom/minutom, `FREQ=WEEKLY` s točno jednim `BYDAY`
te `FREQ=MONTHLY` s `BYMONTHDAY` 1–28. Nepodržani `INTERVAL`, `COUNT`, `UNTIL`,
duplikati i nepoznata polja vraćaju `422`. Prioritet je eksplicitni raspored,
zatim `configuration.cadence`, zatim zadani calendar-gap ili newsletter cadence
iz profila. Tjedni/dvotjedni ritam bez RRULE-a sidri se na petak, mjesečni na
prvi dan, a promjena IANA zone ponovno računa isti deklarirani lokalni sat.
Profile-save i automation create/update serijalizirani su po projektu, reanchor
čuva dvotjednu fazu, a worker svaki RRULE ponovno računa iz deklaracije kako
jednokratna normalizacija proljetnog DST-gapa ne bi trajno pomaknula sat.
Produkcijski image uključuje `tzdata`; migracija 014 sigurno popravlja samo
neizmijenjena, nikad pokrenuta početna sidra postojećih projekata.

Operativni workspace:

- `GET/POST /api/v1/projects/:projectID/automations`
- `PUT/DELETE /api/v1/projects/:projectID/automations/:ruleID`
- `POST /api/v1/projects/:projectID/automations/:ruleID/run`
- `GET/POST /api/v1/projects/:projectID/channel-connections`
- `PUT/DELETE /api/v1/projects/:projectID/channel-connections/:connectionID`
- `POST /api/v1/projects/:projectID/channel-connections/:connectionID/test`
- `GET/POST /api/v1/projects/:projectID/service-requests`
- `PUT /api/v1/projects/:projectID/service-requests/:requestID`
- `GET/POST /api/v1/projects/:projectID/personas`
- `PUT/DELETE /api/v1/projects/:projectID/personas/:personaID`
- `GET/POST /api/v1/projects/:projectID/newsletter/deliveries`

Publika i asistent:

- `GET/POST /api/v1/projects/:projectID/audience/lists`
- `PUT/DELETE /api/v1/projects/:projectID/audience/lists/:listID`
- `GET/POST /api/v1/projects/:projectID/audience/contacts`
- `GET/PUT/DELETE /api/v1/projects/:projectID/audience/contacts/:contactID`
- `POST /api/v1/projects/:projectID/audience/import/csv`
- `GET /api/v1/projects/:projectID/assistant/status`
- `GET/POST /api/v1/projects/:projectID/assistant/threads`
- `GET/POST /api/v1/projects/:projectID/assistant/threads/:threadID/messages`

Administracija, sandbox social i audit:

- `GET/POST /api/v1/projects/:projectID/team`
- `PUT/DELETE /api/v1/projects/:projectID/team/:memberID`
- `GET/POST /api/v1/projects/:projectID/plans`
- `GET/PUT /api/v1/projects/:projectID/entitlement`
- `GET/POST /api/v1/projects/:projectID/social/connections`
- `POST /api/v1/projects/:projectID/social/connections/:connectionID/test`
- `DELETE /api/v1/projects/:projectID/social/connections/:connectionID`
- `GET/POST /api/v1/projects/:projectID/social/posts`
- `GET/POST /api/v1/projects/:projectID/actions`

Odgovori koriste `{"data": ...}`, a greške stabilni oblik
`{"error":{"code":"...","message":"..."}}`. Sve projektne rute provjeravaju
sesiju, članstvo i ulogu. `aiAgents`, `automations`, `auditLog`, dashboard
`analytics` i non-sandbox channel načini (`api`) dodatno se provjeravaju iz aktivnog entitlementa pri
svakom zahtjevu, pa promjena paketa
odmah utječe na pristup. `seatLimit` se provodi pri dodavanju/reaktivaciji člana,
a `monthlyPublicationLimit` pri novoj social objavi, prijelazu content varijante
u scheduled/published i zakazanoj newsletter dostavi; probni newsletter ne
troši kvotu. Potrošnja se upisuje u nepromjenjivi mjesečni ledger, pa naknadna
izmjena `updatedAt` ne može ni dodati ni vratiti slot, a retry istog izvora u
istom UTC mjesecu je idempotentan. `storageLimitBytes` se transakcijski provodi
pri svakom asset uploadu; zbroj se računa iz stvarnih projektnih datoteka, a
upload zahtijeva aktivan ili trial entitlement. `socialChannels`
ograničava broj istodobno aktivnih social sandbox providera, dok `"all"`
uklanja taj limit. Detaljna matrica je u
arhitekturnom dokumentu.

Setup sučelje sprema i izlaže stvarne kontrole za `socialPostsPerWeek`,
`newsletterCadence` i IANA `timezone`. Frontend koristi istu role/feature
matricu za zaključavanje strategy, persona, content/calendar, blog/newsletter,
integracijskih i administrativnih kontrola te blokira ručni hash ulaz u ekran
koji aktivna uloga ili paket ne dopuštaju.

`prioritySupport` serverski označava i stavlja servisne zahtjeve na vrh reda, a
`whiteLabel` mijenja brand u newsletter i web preview izlazu. Custom feature
schema odbija nepoznate ključeve i pogrešne tipove. Inertna `sso` zastavica je
uklonjena: OIDC/SSO se neće nuditi dok nije implementiran cijeli IdP callback i
identity-linking tok.

## AI bez API ključa

Zadani `AI_PROVIDER=local` koristi ugrađeni deterministički engine. U promptni
kontekst ulaze naziv organizacije, spremljene personae (opis i demografija),
cilj, publika, teme, poruka, dokazi, ton, zabranjene tvrdnje i tekst izdvojen iz
strategije. Hrvatski i engleski predlošci slijede `defaultLocale` projekta,
odnosno eksplicitni `language` zahtjeva. Engine radi bez mreže i računa, ali
nije opći jezični model.

Isti provider koristi Content AI i trajni chat asistent. Poruka može
referencirati do pet prethodno učitanih `assistant_attachment` asseta iz istog
projekta; izdvojeni tekst ulazi u privremeni strategy kontekst te poruke. Chat
trenutno može vratiti sažetak stvarnog stanja projekta, pročitati budući
kalendar, izraditi skicu sadržaja i promijeniti `enabled` stanje postojećeg
automation pravila samo kada aktivni paket sadrži `automations=true`.
Telegram/WhatsApp su za sada oznake kanala razgovora; sinkronizacija poruka s
pravim botovima zahtijeva webhook adaptere.

Za bogatiju lokalnu generaciju instalirajte Ollama model i postavite:

```sh
AI_PROVIDER=ollama
OLLAMA_BASE_URL=http://127.0.0.1:11434
OLLAMA_MODEL=your-installed-local-model
```

Ako API radi u Docker Desktopu na macOS-u, koristite
`http://host.docker.internal:11434`. Ollama ne treba cloud račun ni API ključ.
Ako model nije dostupan, Millena vraća upozorenje i koristi ugrađeni engine.

## Pokretanje

Za potpuno novi lokalni PostgreSQL volume:

```sh
docker compose up --build
```

Otvorite `http://127.0.0.1:8080`. PostgreSQL sluša na `5432`, a API i frontend
na `8080`.

Compose SQL datoteke u `/docker-entrypoint-initdb.d` izvršavaju se samo pri
prvom stvaranju praznog PostgreSQL volumea. Na postojećoj bazi nove migracije
primijenite ručno redom; samo `docker compose up --build` ne ponavlja init
skripte. Brisanje volumea (`docker compose down -v`) briše sve lokalne podatke i
prikladno je samo kada su podaci jednokratni.

Ako postojeći Compose volume već ima migracije 001–006, migracije 007–015 za
`companyDescription`, projektne assete, strukturirane persone, content-media
veze, konzistentan newsletter raspored, trajni publication ledger i zonirana
sidra početnih automatizacija primijenite bez brisanja podataka:

```sh
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000007_project_assets.up.sql
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000008_project_personas.up.sql
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000009_content_item_assets.up.sql
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000010_social_publication_cascade.up.sql
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000011_newsletter_schedule_consistency.up.sql
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000012_publication_consumptions.up.sql
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000013_remove_unimplemented_sso_feature.up.sql
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000014_timezone_aware_seed_anchors.up.sql
docker compose exec -T db psql -1 -U millena -d millena -v ON_ERROR_STOP=1 < migrations/000015_starter_ten_publications.up.sql
```

Migracija 014 ponovno sidri samo nikad pokrenuta i neizmijenjena standardna
`calendar_gap`/newsletter pravila. Ugašena, ručno uređena ili već izvršena
pravila ostavlja netaknuta.

Za lokalni Go razvoj postavite vrijednosti iz `.env.example`, pokrenite
PostgreSQL, primijenite sve `migrations/*.up.sql` numeričkim redom i zatim:

```sh
go run ./cmd/api
```

Migracija 015 postavlja katalog početnog Starter paketa na 10 objava mjesečno;
postojeće entitlemente ne mijenja.

API pri pokretanju idempotentno osigurava razvojni MPR workspace. Registracija
novog korisnika u jednoj transakciji stvara zaseban tenant, owner članstvo,
Starter entitlement s 10 objava mjesečno, profil, početnu strategiju i sadržaj, default publiku,
automation pravila, lokalni newsletter sandbox te početni razgovor asistenta.
`POST /projects` također transakcijski dodaje operativni profil, pravila,
publiku, newsletter sandbox i assistant thread; asistent uredno radi i dok novi
projekt još nema ispunjenu strategiju.
