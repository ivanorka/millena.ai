ALTER TABLE content_items
    DROP CONSTRAINT content_items_kind_check,
    ADD CONSTRAINT content_items_kind_check CHECK (
        kind IN ('source', 'social', 'blog', 'newsletter', 'press_release', 'case_study', 'event')
    ),
    ADD COLUMN summary TEXT NOT NULL DEFAULT '',
    ADD COLUMN channels TEXT[] NOT NULL DEFAULT '{}'::text[],
    ADD COLUMN scheduled_for TIMESTAMPTZ,
    ADD COLUMN source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'ai', 'bot', 'import')),
    ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    ADD COLUMN seed_key TEXT;

CREATE UNIQUE INDEX content_items_project_seed_idx
    ON content_items (project_id, seed_key)
    WHERE seed_key IS NOT NULL;

CREATE INDEX content_items_project_kind_idx
    ON content_items (project_id, kind, updated_at DESC);

CREATE TABLE project_strategies (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    mode TEXT NOT NULL DEFAULT 'questions' CHECK (mode IN ('questions', 'upload')),
    six_month_goal TEXT NOT NULL DEFAULT '',
    primary_goals TEXT[] NOT NULL DEFAULT '{}'::text[],
    priority_topics TEXT[] NOT NULL DEFAULT '{}'::text[],
    audience TEXT NOT NULL DEFAULT '',
    audience_problem TEXT NOT NULL DEFAULT '',
    brand_message TEXT NOT NULL DEFAULT '',
    proof_points TEXT NOT NULL DEFAULT '',
    forbidden_topics TEXT NOT NULL DEFAULT '',
    success_metrics TEXT NOT NULL DEFAULT '',
    tone TEXT NOT NULL DEFAULT '',
    source_filename TEXT,
    source_mime_type TEXT,
    source_text TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO project_strategies (
    project_id, mode, six_month_goal, primary_goals, priority_topics, audience,
    audience_problem, brand_message, proof_points, forbidden_topics,
    success_metrics, tone
)
SELECT
    id,
    'questions',
    'Povećati broj kvalitetnih poslovnih upita, vidljivost stručnjaka i kontinuitet komunikacije.',
    ARRAY['Novi poslovni upiti', 'Ugled brenda', 'Vidljivost stručnjaka'],
    ARRAY['Korporativne komunikacije', 'Ljudi i kultura', 'Studije slučaja', 'Događaji'],
    'Direktorice marketinga, komunikacijski timovi i uprave srednjih i velikih organizacija.',
    'Složene poslovne teme treba pretvoriti u jasne, vjerodostojne i korisne poruke.',
    'MPR Grupa spaja strateško razmišljanje, provjerene činjenice i izvedbu koja gradi povjerenje.',
    'Studije slučaja, izjave klijenata, rezultati kampanja, stručni komentari i istraživanja.',
    'Neprovjerene brojke, povjerljivi podaci, politički stavovi i obećanja bez dokaza.',
    'Kvalitetni upiti, doseg stručnjaka, spremanja sadržaja, rast newsletter publike i konverzije.',
    'Stručno i samouvjereno, ali pristupačno; jasno, konkretno i bez generičkih marketinških fraza.'
FROM projects
WHERE slug = 'millena-demo'
ON CONFLICT (project_id) DO NOTHING;

INSERT INTO content_items (
    project_id, author_id, kind, status, title, summary, body, channels,
    scheduled_for, source, metadata, seed_key
)
SELECT project.id, member.user_id, seed.kind, seed.status, seed.title, seed.summary,
       seed.body, seed.channels, seed.scheduled_for, seed.source,
       jsonb_build_object('seeded', true, 'strategyRevision', 1), seed.seed_key
FROM projects AS project
JOIN LATERAL (
    SELECT user_id
    FROM project_members
    WHERE project_id = project.id AND role = 'owner'
    ORDER BY created_at
    LIMIT 1
) AS member ON true
CROSS JOIN LATERAL (
    VALUES
      ('social', 'in_review', 'Kako povjerenje nastaje prije prve kampanje',
       'LinkedIn objava s jasnim stavom i pitanjem za komunikacijske timove.',
       'Povjerenje se ne gradi jednom velikom kampanjom. Gradi se svakim jasnim odgovorom, svakom provjerenom činjenicom i svakim trenutkom u kojem organizacija pokaže da sluša. Koji mali signal povjerenja vaša publika danas najviše treba?',
       ARRAY['linkedin'], now() + interval '1 day', 'ai', 'mpr-social-trust'),
      ('social', 'draft', 'Tri pitanja prije krizne objave',
       'Praktična carousel tema za LinkedIn i Instagram.',
       'Prije objave u kriznoj situaciji provjerite tri stvari: znamo li što se dogodilo, možemo li tvrdnju dokazati i pomaže li ova poruka ljudima koji je čitaju? Brzina je važna, ali vjerodostojnost ostaje duže.',
       ARRAY['linkedin','instagram'], NULL, 'manual', 'mpr-social-crisis'),
      ('blog', 'draft', 'Pet trendova koji mijenjaju odnose s javnošću',
       'Analitički članak za web s primjerima primjene u komunikacijskim timovima.',
       'Komunikacijski timovi rade u okruženju u kojem se očekivanja publike mijenjaju brže od godišnjih planova. U članku obrađujemo kontinuirani razgovor, dokazivu stručnost, first-party podatke, odgovornu automatizaciju i novu ulogu urednika.',
       ARRAY['blog'], NULL, 'ai', 'mpr-blog-trends'),
      ('newsletter', 'scheduled', 'Tjedni pregled: ljudi, projekti i ideje',
       'Urednički odabir najboljih uvida iz projekta za postojeću publiku.',
       'Ovaj tjedan izdvajamo lekciju iz krizne komunikacije, pogled iza kulisa produkcije i tri pitanja koja pomažu pretvoriti složenu temu u korisnu poruku.',
       ARRAY['newsletter'], date_trunc('week', now()) + interval '4 days 10 hours', 'ai', 'mpr-newsletter-weekly'),
      ('press_release', 'approved', 'MPR Grupa širi tim za integrirane komunikacije',
       'Priopćenje s naglaskom na novu ekspertizu i korist za klijente.',
       'Zagreb — MPR Grupa proširila je tim za integrirane komunikacije kako bi klijentima povezala strateško savjetovanje, sadržaj i digitalnu distribuciju u jedinstven radni proces. Nova struktura jača brzinu izvedbe bez kompromisa u provjeri činjenica i kvaliteti odobravanja.',
       ARRAY['media','blog'], now() + interval '3 days 11 hours', 'manual', 'mpr-press-team'),
      ('case_study', 'in_review', 'Od stručnog događaja do mjesec dana relevantnog sadržaja',
       'Studija slučaja strukturirana kroz izazov, pristup i mjerljiv rezultat.',
       'Izazov: vrijedni uvidi s događaja nestajali su nakon jednog dana. Pristup: razgovore, fotografije i izjave pretvorili smo u povezani niz objava, članak i newsletter. Rezultat: tim je dobio konzistentan sadržaj za cijeli mjesec i jasnu osnovu za daljnje mjerenje.',
       ARRAY['blog','linkedin','newsletter'], NULL, 'manual', 'mpr-case-event'),
      ('event', 'scheduled', 'Komunikacije koje grade povjerenje — otvoreni studio',
       'Najava stručnog susreta za klijente i komunikacijsku zajednicu.',
       'Otvaramo studio za razgovor o komunikaciji koja ostaje vjerodostojna i kada se kanali, alati i očekivanja brzo mijenjaju. Program uključuje kratke primjere iz prakse, pitanja publike i konkretan radni okvir za idući kvartal.',
       ARRAY['linkedin','newsletter'], now() + interval '6 days 9 hours', 'manual', 'mpr-event-studio'),
      ('source', 'draft', 'Intervju sa stručnjakinjom za ESG komunikaciju',
       'Izvorni materijal koji tek treba pretvoriti u formate za odabrane kanale.',
       'Bilješke: razlikovati regulatorne obveze od komunikacijskih preporuka; koristiti samo provjerene podatke; pripremiti citat, FAQ i kratki LinkedIn sažetak.',
       ARRAY[]::text[], NULL, 'bot', 'mpr-source-esg')
) AS seed(kind, status, title, summary, body, channels, scheduled_for, source, seed_key)
WHERE project.slug = 'millena-demo'
ON CONFLICT (project_id, seed_key) WHERE seed_key IS NOT NULL DO NOTHING;
