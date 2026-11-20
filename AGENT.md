# Receipt Studio Agent

Go agent s nativnim MyGo interfejsom i javnim API-jem za povezane aplikacije. Web UI i administratorski HTTP API ne postoje. Nativni interfejs poziva Go metode direktno. Node i browser nisu potrebni za rad agenta.

## Pokretanje i printeri

```sh
bash scripts/dev-native.sh --listen 127.0.0.1:17842 --data-dir "$PWD/.native-dev"
```

Podrazumijevana adresa bez ovih argumenata je `127.0.0.1:17840`. Agent dopušta samo loopback listener. Web aplikacija šalje zahtjev s korisničkog računara; udaljeni server ne može pristupiti njegovom localhostu.

U nativnom prozoru napravi predložak, dodaj printer i odaberi mu predložak. Prvi printer je podrazumijevani.

- **LAN / Ethernet:** privatna IP adresa i TCP port, obično 9100. Podrazumijevani transport je TCP.
- **USB:** odaberi sistemski red iz dropdowna nakon dodavanja printera u operativnom sistemu. Lista prikazuje sve sistemske redove, uključujući mrežne. macOS/Linux koriste CUPS RAW; Windows koristi Winspool RAW. Potreban je red/driver koji prosljeđuje komande bez konverzije.

Provjera veze otvara TCP port ili provjerava sistemski red, bez slanja računa. Probni račun i ponavljanje posla traže potvrdu. USB transport koristi zaseban proces kako bi se blokirani driver mogao prekinuti.

## Javni API

Dostupne su četiri rute:

| Metoda i ruta | Namjena |
|---|---|
| `GET /v1/health` | Provjera rada agenta |
| `POST /v1/print-jobs` | Prihvatanje računa za printanje |
| `GET /v1/print-jobs/{id}` | Sažetak statusa posla |
| `GET /v1/print-jobs/{id}/receipt` | Preuzimanje slike računa kao binarni `image/png` |

Svaki zahtjev koristi `Authorization: Bearer <API_KEY>`. POST zahtijeva `Content-Type: application/json`. API ključ se čuva u SQLite bazi i može se rotirati kroz nativni interfejs. Ne daje pristup administraciji, predlošcima ili payloadu sačuvanih poslova.

### POST /v1/print-jobs

```json
{
  "version": 1,
  "job_id": "invoice-42-print-1",
  "printer_id": "printer-a1b2c3d4",
  "data": {
    "shop": "Sample Store",
    "address": "Sarajevo",
    "number": "2026/0042",
    "date": "04.11.2026. 14:00",
    "items": [
      { "name": "Coffee", "qty": "1", "price": "15,00" }
    ],
    "total": "15,00 KM"
  }
}
```

`printer_id` je opcionalan; koristi se podrazumijevani printer. ID konkretnog printera uključen je u kopirani JS primjer. `job_id` dopušta slova, brojeve, `-` i `_`, najviše 100 znakova. Limit cijelog zahtjeva je 1 MB.

- **200 uz `return_png: true`:** posao i PNG su sačuvani; odgovor uključuje `png` objekt. Status printanja je zasebno polje (`queued`, `sent` itd.).
- **202 (bez PNG-a):** posao je validiran, enkodiran i trajno sačuvan, čeka slanje.
- **200, duplicate: true:** isti ID i isti podaci već postoje; ne printaju se ponovo.
- **409:** isti ID ima druge podatke ili eksplicitno drugi printer.
- **422:** predložak/podaci nisu kompatibilni, printer nije podešen.
- **401:** API ključ nedostaje ili nije važeći.

HTTP odgovor sadrži `id`, `status`, `message`, `printer_id`, `printer_name`, `template_version`, `byte_count`, `created_at`, `updated_at`, `duplicate`. Receipt podaci se ne vraćaju preko javnog API-ja.

### GET /v1/print-jobs/{id}/receipt

Preuzima sliku računa bez Base64 omotača. Koristi isti API ključ i ID postojećeg
posla; ne printa ponovo niti mijenja status posla. Dostupno je za svaki sačuvani
status, uključujući `queued`, `failed` i `unknown`; slika ne potvrđuje fizičko
printanje. Ako PNG još nije sačuvan, generiše se iz originalnih komandi/profila i
čuva za naredna preuzimanja. Ovo radi i kada prvobitni POST nije tražio `return_png`.

```sh
curl --fail --show-error \
  -H "Authorization: Bearer $API_KEY" \
  "http://127.0.0.1:17840/v1/print-jobs/invoice-42-print-1/receipt" \
  --output receipt.png
```

Uspjeh vraća **200**, `Content-Type: image/png`, tačan `Content-Length` i
`Content-Disposition: attachment; filename=invoice-42-print-1.png`. Tijelo su
izvorni PNG bajtovi. U browseru koristi `response.blob()` ili
`response.arrayBuffer()`, a ne `response.json()`. U Postmanu odgovor možeš
pregledati kao sliku ili sačuvati u fajl. `HEAD` vraća ista zaglavlja bez tijela.

Greške vraćaju JSON s poljem `error`: **401** za nevažeći ključ, **404** za
nepostojeći/neispravan ID, **422** kada pregled nije moguće generisati i **500**
za grešku baze. Neuspjelo preuzimanje možeš ponoviti istim GET-om, bez novog
printanja. Uspješna čitanja ne pune dijagnostiku; greške preuzimanja se bilježe.

Postojeći `return_png` odgovor ostaje dostupan i ne mijenja format. Binarna ruta
uklanja Base64 proširenje, ali trenutno učitava cijeli komprimirani PNG u Go
memoriju; nije streaming iz baze i ne uklanja rok od 45 sekundi za odgovor.

### Opcionalni PNG računa

U POST payload dodaj `"return_png": true`. Uspješan zahtjev vraća HTTP **200** i postojeća polja posla, uz:

```json
{
  "id": "invoice-42-print-1",
  "status": "queued",
  "duplicate": false,
  "png": {
    "content_type": "image/png",
    "encoding": "base64",
    "width": 640,
    "height": 842,
    "dpi": 203.2,
    "scale": 1,
    "paper_width_mm": 80,
    "margin_left": 32,
    "margin_right": 32,
    "data": "iVBORw0KGgo..."
  }
}
```

Visina iz primjera je ilustrativna; stvarna zavisi od računa. `png.data` je Base64 bez `data:` prefiksa. Dekodiraj ga u bajtove i sačuvaj kao `.png`. U browseru:

```js
const result = await response.json();
if (!response.ok) throw new Error(result.error);
if (result.png) {
  const pngBytes = Uint8Array.from(atob(result.png.data), c => c.charCodeAt(0));
  const pngFile = new Blob([pngBytes], { type: result.png.content_type });
  // pngFile možeš poslati svom backendu ili sačuvati.
  imageElement.src = `data:${result.png.content_type};base64,${result.png.data}`;
}
```

PNG predstavlja cijeli papir: **80 mm = 640 px**, područje printanja 576 px i bijele margine po 32 px (4 mm); profil 58 mm daje 464 px, područje 384 px i margine po 40 px (5 mm). Ovo su centrirane margine početnih profila, a ne kalibracija određenog modela. Tekst se interno renderuje u 3× rezoluciji po horizontalnim trakama od 512 dots, s preklapanjem za Catmull-Rom filter, pa kvalitetno smanjuje uz zaglađene ivice; QR/logo zadržavaju izvorne oštre piksele. PNG `pHYs` zapis sadrži 8000 piksela po metru (203,2 DPI), pa program koji poštuje fizičku rezoluciju može prikazati/isprintati sliku široku 80 mm. Prikaz na ekranu ipak zavisi od zooma i aplikacije. `width`/`height` opisuju izlazne piksele, `scale` je 1, a `paper_width_mm` i `margin_left`/`margin_right` opisuju papir i margine u pikselima. PNG prikazuje tekst, tabele, stvarni QR i logo te završni pomak papira. Nema kontrola editora ni generičkog QR simbola. Izvor su **sačuvani bajtovi poslani printeru**, a ne naknadno izmijenjeni predložak. Raspored, veličine, poravnanje i grafički pikseli prate komande. Ugrađeni Go Mono približno prikazuje font A: oblik slova i firmwareov izbor nativnog QR uzorka nisu garancija identičnosti fizičkom otisku. Za QR s tačno određenim pikselima koristi QR kao sliku. Bočne margine papira su dodane; mehanička udaljenost rezača od glave nije kalibrisana.

Bez `return_png` ili uz `false`, novi posao i dalje vraća **202** bez slike. HTTP 200 uz sliku znači da je zahtjev obrađen i posao sačuvan; ne potvrđuje fizičku printanje. `GET /v1/print-jobs/{job_id}` ostaje lagana provjera statusa bez PNG-a.

Za ponovno preuzimanje ponovi isti POST (`job_id`, podaci i printer), uz `return_png: true`. To ne šalje račun ponovo. PNG možeš zatražiti i naknadno za posao koji ga prvobitno nije tražio; koristi originalne sačuvane komande/profil. Jednom generisan PNG se čuva u poslu i ostaje isti nakon restarta, promjene predloška ili nadogradnje renderera. Drugi podaci za isti ID i dalje daju 409.

3× radna traka i radni prostor filtera ponovo se koriste za cijeli račun; cijela slika ostaje u memoriji samo u konačnoj 1× rezoluciji. Granica od 64 miliona piksela odnosi se na konačni papir s marginama, uz maksimalnu visinu 100.000 dots. Granica zavisi od dimenzija računa, ne od broja stavki. Tekst, QR/logo i podvlačenje zadržavaju isti raspored i kvalitet. Ako generisanje PNG-a novog posla ne uspije, vraća se 422 i posao se ne dodaje u red za printanje. Ako je riječ o već prihvaćenom poslu, neuspjeh PNG-a ne mijenja njegov status i ne izaziva ponovno slanje. Opcija radi s postojećim API ključem; nema vanjskog servisa, browsera niti mrežnog učitavanja fontova.

### GET /v1/print-jobs/{job_id}

Vrati status uz isti API ključ. Ponovno slanje istog job_id nakon HTTP timeouta je sigurno na nivou reda poslova. Nemoj za svaki mrežni retry generisati novi ID. Za namjernu novu kopiju koristi novi ID.

### GET /v1/health

Autentificirana provjera da agent radi; ne tvrdi da je printer dostupan.

## Statusi i oporavak

`queued → sending → sent / failed / unknown`

- `queued`: posao je trajno sačuvan i čeka slanje.
- `sending`: slanje je u toku.
- `sent`: bajtovi su predati vezi ili sistemskom redu; nije potvrda fizičkog printanja.
- `failed`: slanje nije uspješno započelo, bez poslanih bajtova.
- `unknown`: ishod slanja nije pouzdan, npr. prekid nakon djelimičnog slanja ili restart tokom slanja.

Jedan worker atomski preuzima jedan posao iz SQLite reda. Status `sending` trajno se sprema prije slanja printeru. Neuspješan upis ishoda pauzira red; agent ponavlja samo upis, nikada slanje računa. Spremanje PNG-a ne mijenja status. Jedan worker obrađuje poslove redom. Pri restartu nastavlja `queued` poslove, a `sending` pretvara u `unknown` bez automatskog ponavljanja. USB sistemski red može naknadno isprintati prihvaćen posao; prije ponavljanja provjeri red i printer.

**Ponovi** kreira novi ID i koristi originalne komande, podatke, predložak i vezu printera. Čuva referencu `original_job_id`. Naknadno uređivanje printera ili predloška ne mijenja postojeći posao. Aktivni posao se ne može ponoviti.

## Lokalni pregled i filteri

Poslovi printanja prikazuju status, vrijeme prijema i obrade, printer, verziju predloška, broj bajtova i poruku. Modal sadrži payload i PNG pregled. Otvaranje pregleda ne šalje račun printeru.

SQLite primjenjuje filtere po statusu i vremenu i paginaciju direktno nad indeksiranim kolonama. Lista i API status čitaju samo metapodatke, bez payloada, komandi i PNG-a. Cursor koristi vrijeme i ID posla. Nativni interfejs učitava po 100 rezultata pri skrolanju; maksimalna veličina jednog internog upita je 500. Nema ukupnog limita broja sačuvanih poslova. Brojači statusa računaju se u bazi nad metapodacima.

Prva stranica automatski se osvježava. Nakon učitavanja starijih stranica automatsko osvježavanje pauzira kako ne bi pomjeralo historiju; dugme za osvježavanje vraća prvu stranicu. Interfejs zadržava samo učitane sažetke. Kompletan račun učitava se pojedinačno za detalje, pregled, slanje ili ponavljanje. Provjera duplog `job_id` koristi primarni ključ i podatke samo tog posla.

`GET /v1/health` vraća i `app_version` iz `mygo.json`; polje `version: 1` i dalje označava verziju API protokola.

## Dijagnostika

Dijagnostika je skrivena pri svakom pokretanju. Sedam klikova na verziju aplikacije
pri dnu sidebara unutar pet sekundi uključuje prikaz; narednih sedam ga isključuje.
Nema PIN-a i ovo nije kontrola ovlaštenja. Klik na naziv Receipt Studio i dalje vodi
na početnu stranicu. Skrivanje prikaza ne zaustavlja zapisivanje događaja; oba
toggle događaja imaju `info` zapis. Ako je Dijagnostika otvorena pri isključivanju,
agent se vraća na Printere. Stanje vidljivosti se ne sprema u bazu.

Nativni ekran **Dijagnostika** prikazuje tehničke događaje od najnovijeg prema starijim. Podržava pretragu poruka i ID-ja posla, filtere po nivou, komponenti i vremenskom rasponu te učitavanje dodatnih stranica pri skrolanju. **Primijeni** je aktivan kada su filteri promijenjeni; osvježavanje ponovo učitava najnovije zapise za primijenjene filtere.

Agent čuva posljednjih 15 dana dijagnostike. Pri pokretanju i zatim svaka 24 sata briše zapise starije od 15 × 24 sata, bez obzira na vidljivost ekrana. Brisanje koristi UTC vrijeme i ne dira poslove, predloške ili postavke. Uspješno brisanje bilježi broj obrisanih zapisa; neuspjeh dobija `error` zapis. Ako je agent ugašen, čišćenje se nadoknađuje pri pokretanju. SQLite može ponovo koristiti oslobođene stranice; datoteka baze se ne smanjuje automatski.

Klik na zapis otvara punu poruku, vrijeme, nivo, komponentu i ID. **Kopiraj zapis** kopira detalje; **Otvori posao** vodi do povezanog računa bez ponovnog printanja. Dijagnostika nema javni HTTP endpoint.

Stvarni događaji koji se zapisuju:

| Akcija | Kada se zapisuje | Nivo / komponenta |
| --- | --- | --- |
| Pokretanje | Nakon otvaranja SQLite baze i učitavanja konfiguracije; zatim pokretanje agenta | `info` / `database`, `agent` |
| Gašenje | Nakon završetka HTTP servera i workera; nasilno gašenje procesa ne može zapisati ovaj događaj | `info` / `agent`; neočekivan izlaz API servera: `error` / `api` |
| Jezik | Nakon uspješnog spremanja stvarne promjene BS/EN; isti jezik ne stvara zapis | `info` / `agent` |
| API ključ | Nakon uspješnog spremanja novog ključa | `info` / `agent` |
| Printer | Nakon dodavanja ili spremanja postavki, uz ID i reviziju | `info` / `printer` |
| Provjera veze | Poslije stvarne TCP/USB provjere; TCP dostupnost ili postojanje sistemskog reda nije potvrda fizičkog printanja | `info` / `printer`; neuspjela veza: `error`; neispravne postavke: `warning` |
| Predlošci | Kreiranje (uključujući dupliranje), spremanje, uvoz, brisanje i vraćanje verzije, tek nakon upisa u bazu; ID i verzija kada postoje | `info` / `agent` |
| Izvoz predloška | Tek nakon uspješnog pisanja izabrane datoteke; otkazivanje dijaloga se ne bilježi | `info` / `agent`; greška: `error` ili `warning` |
| Odbijene administrativne izmjene | Neispravan unos/predložak, nedostajući objekt ili konflikt revizije; uključuje odbijeno ponovno printanje aktivnog posla | `warning` / `agent` ili `printer` |
| Neuspjelo spremanje | Jezik, ključ, printer, predložak/verzija, novi posao, kopija ili PNG nisu sačuvani | `error` / `database`; bez lažne potvrde uspjeha |
| API prihvatanje | Novi posao je sačuvan i stavljen u red | `info` / `api`, povezan s poslom |
| API duplikat | Isti `job_id` i podaci vraćaju postojeći posao; nema novog printanja | `info` / `api`, povezan s postojećim poslom |
| API odbijanje | Nevažeći ključ (401), Host/preflight (403), JSON/Content-Type/veličina (400), konflikt `job_id` (409), neodgovarajući podaci/postavke (422) | `warning` / `api` |
| API greška spremanja | Novi posao ili PNG postojećeg posla nisu sačuvani; odgovor HTTP 500 | `error` / `database` |
| Probni račun / ponavljanje | Novi posao/kopija je sačuvan i stavljen u red; kopija navodi izvorni ID | `info` / `printer`, povezan s novim poslom |
| Slanje | Status `sending` je sačuvan, neposredno prije poziva transporta | `info` / `printer`, povezan s poslom |
| Ishod slanja | `sent`: bajtovi poslani ili sistemski red prihvatio posao; `failed`: slanje nije uspjelo; `unknown`: ishod nije siguran | `info` / `error` / `warning`, komponenta `printer` |
| Greška spremanja statusa | Status prije slanja ili ishod slanja nisu sačuvani; pri grešci prije slanja transport se ne poziva | `error` / `database`, povezan s poslom |
| Oporavak nakon prekida | Pri pokretanju posao zatečen u `sending` postaje `unknown`, bez automatskog ponavljanja | `warning` / `printer`, povezan s poslom |
| Neočekivane interne greške | Postojeći pozivi standardnog Go loggera | `error` / `agent` |

Dijagnostika ne sprema request body, sadržaj predloška, primjer podataka ili API ključ. Odbijeni API zahtjevi imaju fiksne opise bez sirovih korisničkih vrijednosti. Uspješni pregledi/listanja, provjere `health`, čitanja statusa poslova, nespremljene izmjene editora i otkazani dijalozi ne stvaraju zapise. Updater je još simulacija i ne stvara stvarne dijagnostičke događaje.

Zapisi su pomoćna dijagnostika: ako SQLite ne može prihvatiti ni dijagnostički upis (npr. disk je pun), greška se ispisuje na stderr; prethodno uspješna akcija se ne poništava. DEMO seed skripta je uklonjena. Nakon akcije otvori ili osvježi Dijagnostiku da učitaš nove zapise.

## Predlošci i historija

Predlošci su odvojeni od printera. Više printera može koristiti isti predložak. Njegova promjena važi za buduće poslove. Dodijeljeni predložak nije moguće obrisati dok se printerima ne odabere drugi.

Izvoz je paket `receipt-studio-template`, verzija 2, s poljima `format`, `version`, `template` i `sample_data`. Uvoz prihvata samo taj format i uvijek kreira novi predložak. `sample_data` je obavezan JSON objekt; može biti eksplicitno prazan. Samostalni dizajn JSON i paket v1 nisu podržani. Unutrašnja šema dizajna ima vlastitu verziju 1.

Svako spremanje čuva potpuni dizajn i primjer podataka kao novu verziju. Historija ima pregled pojedinačne verzije i stranice od 50 zapisa. Vraćanje kopira odabranu verziju u novu verziju, bez prepisivanja prethodnih. Zastarjela izmjena se odbija. Brisanje predloška zadržava historiju; njegov ID se ne može ponovo koristiti.

## Spremanje

Podaci se nalaze u `os.UserConfigDir()/ReceiptStudio`, na macOS-u `~/Library/Application Support/ReceiptStudio`. Razvojni primjer koristi odvojeni `.native-dev` folder.

Baza `receipt-studio.sqlite` ima osam tabela:

| Tabela | Sadržaj |
|---|---|
| `settings` | Pojedinačne postavke: API ključ, podrazumijevani printer, jezik, oznaka desktop podešavanja i otisak šeme; nema konfiguracijskog JSON bloba |
| `printers` | Naziv, LAN/USB veza, dodijeljeni predložak, revizija postavki i redoslijed |
| `templates` | Identitet predloška, broj trenutne verzije, redoslijed i oznaka brisanja; brisanje skriva predložak i zadržava historiju |
| `template_versions` | Potpuni dizajn i primjer podataka svake verzije, vrijeme i verzija iz koje je vraćena |
| `jobs` | Metapodaci: ID, printer, verzija predloška, status, poruka, vremena, broj bajtova i izvorni ID pri ponavljanju; indeksi za vrijeme/ID i status/vrijeme/ID |
| `job_receipts` | Veza 1:1 s poslom: originalna adresa/USB red, predložak, payload i komande kao binarni BLOB |
| `job_pngs` | Opcionalni PNG kao sirovi binarni BLOB, dimenzije, DPI, širina papira, margine i skala; veza 1:1 s računom |
| `diagnostics` | Vrijeme, nivo (`info`, `warning`, `error`), komponenta, poruka i opcionalni ID posla |

Nova instalacija inicijalizira samo praznu, neverzionisanu SQLite bazu. Normalizovana šema je početna verzija 1 (`PRAGMA user_version`); njen otisak se čuva u `settings.schema-fingerprint`. Razvojni reset je uklonjen: nepodržana verzija, nepoznata šema ili nedostajući otisak prijavljuju grešku bez brisanja postojećih podataka. Nedostajući API ključ u bazi koja već sadrži podatke ne stvara novu konfiguraciju. Starije razvojne baze zahtijevaju zaseban, izričito odobren prijenos ili novi direktorij podataka; ne uvoze se niti resetuju automatski. Buduće promjene šeme zahtijevaju numerisane migracije koje čuvaju podatke, s promjenom `user_version` u istoj transakciji. Trenutno nema migracija nakon početne šeme 1. Verzija aplikacije je odvojena od verzije baze.

Printeri i predlošci su zasebni zapisi. Spremanje mijenja samo izmijenjene redove, zajedno s novom verzijom predloška u jednoj transakciji. Dizajn i primjer podataka ostaju JSON u `template_versions`; trenutni dizajn se ne duplicira u `settings`. Uklonjeni su limiti od 10 printera i 100 predložaka. Trenutna konfiguracija se još učitava u memoriju za interfejs; normalizacija sama ne virtualizuje njegove liste.

Slanje printeru učitava samo komande i vezu. Detalji učitavaju metapodatke i payload, bez komandi i PNG-a. Slika se čita tek pri otvaranju pregleda ili zahtjevu `return_png`; ako ne postoji, nastaje iz sačuvanih komandi/profila i čuva se za naredna čitanja. Base64 se pravi pri JSON serijalizaciji API odgovora, ne pri spremanju u bazu. Ponovno printanje kopira postojeći PNG direktno unutar SQLite-a, bez učitavanja slike u Go.

Posao, njegov račun i eventualni PNG nastaju u jednoj transakciji; postojeći ID se ne prepisuje. Historija računa i PNG trenutno nema automatsko arhiviranje niti brisanje. Dijagnostika ima zasebno zadržavanje od 15 dana.

SQLite koristi transakcije, WAL i `synchronous=FULL`. Datoteke `-wal` i `-shm` pripadaju bazi, a `agent.lock` sprečava paralelan rad dva agenta nad istim podacima. Folder ima Unix dozvole 0700, nova baza 0600; Windows koristi korisnički ACL. Baza nije dodatno šifrovana. Za backup zaustavi agent ili koristi SQLite backup API.

Korisnički izvozi JSON/BIN/PNG su fajlovi. OS autostart koristi sistemsku registraciju. Bazu, API ključeve i izvoz stvarnih poslova ne stavljati u Git.

## Browser integracije i HTTPS

API prihvata zahtjeve s različitih origina uz važeći ključ. CORS preflight ne zahtijeva ključ, stvarni zahtjevi ga zahtijevaju. To ne uklanja browser ograničenja za lokalnu mrežu; integraciju treba provjeriti u ciljanom browseru.

Opcionalni HTTPS koristi `--tls-cert /path/localhost.pem --tls-key /path/localhost-key.pem`. Klijent mora vjerovati certifikatu koji pokriva korišteno ime/IP. Agent ne generiše niti instalira certifikate.

## Desktop i distribucija

Tray meni otvara nativni Receipt Studio prozor, upravlja pokretanjem pri prijavi i gasi agent. Zatvaranje prozora ga sakriva; agent nastavlja rad. Autostart radi pri prijavi korisnika, ne kao sistemski servis prije prijave.

MyGo pakovanje definišu `mygo.json` i `scripts/build-mygo.sh`; macOS/Windows skripte delegiraju tom alatu. Stare zasebne installer skripte nisu dio projekta. Release buildovi se rade samo na izričit zahtjev. Automatski updater i distribucijsko potpisivanje još nisu podešeni. Razvoj, testovi i simulacija obavijesti o nadogradnji opisani su u [NATIVE.md](NATIVE.md).
