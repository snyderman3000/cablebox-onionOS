CABLEBOX — TV guide and Sonarr remote for OnionOS (Miyoo Mini Plus)
==================================================================
Built by Claude, Anthropic's AI model, for snyderman3000.
Source, updates and bug reports: https://github.com/snyderman3000/cablebox

Cablebox lists TV shows on channels: new series, on this week, popular,
top rated, one channel per streaming service and one per genre. Each show
has IMDb, Rotten Tomatoes and Metacritic scores when available, seasons,
cast, where it streams and a trailer QR code. Order a show into Sonarr:
new episodes only, every episode, chosen seasons or chosen episodes.

INSTALL
  1. Copy the "App" folder from this zip to the root of your SD card
     (merge it with the App folder that's already there).
  2. Keys: if Rewind is installed and set up, Cablebox uses its TMDB and
     OMDb keys. Otherwise copy App/Cablebox/config.example.json to
     App/Cablebox/data/config.json and add them:
       TMDB  https://www.themoviedb.org/settings/api   (required)
       OMDb  https://www.omdbapi.com/apikey.aspx      (IMDb ratings)
  3. To order shows, add Sonarr to the same file:
       "sonarr_url": "192.168.4.34:8989",
       "sonarr_api_key": "from Sonarr → Settings → General"
  4. Turn Wi-Fi on and open Apps → Cablebox.

CONTROLS
  GUIDE     Up/Down move · Left/Right page · A info · X order
            L1/R1 channel · L2/R2 group · START channel guide
            Y sort · SELECT refresh · B off
  DETAILS   X order/manage · Y trailer · Up/Down scroll
            L1/R1 previous/next · B back
  SEASONS   A tick · X all/none · START order · B back
  EPISODES  Left/Right season · A tick · X whole season
            START order · B back
  MENU turns it off from anywhere.

SETTINGS  (App/Cablebox/data/config.json)
  tmdb_api_key, omdb_api_key   blank = borrow Rewind's
  region, language             default US, en-US
  services                     which services get channels, by name
  refresh_hours                default 6
  sonarr_url, sonarr_api_key   your Sonarr
  sonarr_quality_profile       name or id (blank = first)
  sonarr_root_folder           e.g. /media/TV (blank = first)
  sonarr_search                false = don't search when ordering

  In the guide, ● means episodes are on disk and ○ means ordered but
  nothing downloaded yet. Ordering a show you already have changes what
  Sonarr monitors.

LOGS: App/Cablebox/data/cablebox.log

DATA
  Show data and images: TMDB. This product uses the TMDB API but is not
  endorsed or certified by TMDB. Streaming availability: JustWatch, via
  TMDB. Ratings: OMDb API. Not affiliated with IMDb, Rotten Tomatoes or
  Metacritic.

Fonts: Share Tech Mono, VT323, Orbitron (SIL Open Font License).
CA bundle: Mozilla via curl (MPL 2.0). QR encoder from rsc.io/qr (BSD).
Licenses are in the licenses folder.
