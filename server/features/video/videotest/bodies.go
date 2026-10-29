package videotest

// The season fetch appends the season to the show root — one request, so the
// path-keyed ProviderStub serves show details AND season details from the same
// "/3/tv/{id}" entry. One body carrying the root fields, a seasons array and
// the "season/{n}" sub-documents validly answers both endpoints. Like the real
// appended document, the seasons carry only a string _id — no integer id.
const ShowWithSeasonBody = `{
  "id": 1396, "name": "Breaking Bad",
  "first_air_date": "2008-01-20",
  "seasons": [
    {"season_number": 1, "name": "Season 1", "episode_count": 7, "air_date": "2008-01-20"},
    {"season_number": 2, "name": "Season 2", "episode_count": 13, "air_date": "2009-03-08"}
  ],
  "season/1": {
    "_id": "52542272760ee31328000094", "season_number": 1, "name": "Season 1",
    "air_date": "2008-01-20", "vote_average": 8.1,
    "episodes": [
      {"episode_number": 1, "name": "Pilot", "air_date": "2008-01-20",
       "runtime": 58, "still_path": "/p1.jpg", "vote_average": 8.2, "vote_count": 1500}
    ]
  },
  "season/2": {
    "_id": "52542273760ee31328000635", "season_number": 2, "name": "Season 2",
    "overview": "Walt gets in deeper.", "air_date": "2009-03-08",
    "poster_path": "/s2.jpg", "vote_average": 8.3,
    "episodes": [
      {"episode_number": 1, "name": "Seven Thirty-Seven", "overview": "A grim omen.",
       "air_date": "2009-03-08", "runtime": 47, "still_path": "/e1.jpg",
       "episode_type": "standard", "vote_average": 8.1, "vote_count": 900,
       "guest_stars": [
         {"id": 62912, "name": "Raymond Cruz", "character": "Tuco Salamanca", "order": 8, "profile_path": "/rc.jpg"},
         {"name": "", "character": "Nobody", "order": 0},
         {"name": "Max Arciniega", "character": "Krazy-8", "order": 3}
       ]},
      {"episode_number": 2, "name": "", "overview": "",
       "air_date": "", "runtime": 0, "still_path": "",
       "vote_average": 0, "vote_count": 0},
      {"episode_number": 3, "name": "Break-In", "episode_type": "mid_season",
       "air_date": "2009-03-22", "runtime": 47, "vote_average": 7.9, "vote_count": 700},
      {"episode_number": 4, "name": "ABQ", "episode_type": "finale",
       "air_date": "2009-05-31", "runtime": 47, "vote_average": 9.0, "vote_count": 1100}
    ]
  },
  "season/2/credits": {"cast": [
    {"id": 84497, "name": "Aaron Paul", "character": "Jesse Pinkman", "order": 1, "profile_path": "/ap.jpg"},
    {"name": "", "character": "Nobody", "order": 0},
    {"name": "Bryan Cranston", "character": "Walter White", "order": 0}
  ]},
  "season/2/aggregate_credits": {"cast": [
    {"name": "Aaron Paul", "order": 1, "profile_path": "/ap.jpg", "total_episode_count": 4,
     "roles": [{"character": "Jesse Pinkman", "episode_count": 4}]},
    {"name": "", "order": 0, "total_episode_count": 1,
     "roles": [{"character": "Nobody", "episode_count": 1}]},
    {"name": "Bryan Cranston", "order": 0, "total_episode_count": 4,
     "roles": [{"character": "Heisenberg", "episode_count": 1},
               {"character": "Walter White", "episode_count": 4}]}
  ]}
}`

type PageBody struct {
	Entries    []map[string]any `json:"entries"`
	Page       int              `json:"page"`
	TotalPages int              `json:"total_pages"`
}

const MatrixDetailsBody = `{
  "id": 603,
  "title": "The Matrix",
  "original_title": "The Matrix",
  "release_date": "1999-03-30",
  "runtime": 136,
  "overview": "A hacker learns the truth.",
  "tagline": "Free your mind.",
  "genres": [{"id": 28, "name": "Action"}, {"id": 878, "name": "Science Fiction"}],
  "poster_path": "/m.jpg",
  "backdrop_path": "/b.jpg",
  "vote_average": 8.2,
  "vote_count": 26000,
  "homepage": "https://thematrix.example",
  "original_language": "en",
  "budget": 63000000,
  "revenue": 463517383,
  "popularity": 85.3,
  "external_ids": {"imdb_id": "tt0133093", "wikidata_id": "Q83495"},
  "videos": {"results": [
    {"key": "m8e-FF8MsqU", "name": "Teaser", "site": "YouTube", "type": "Teaser", "official": true, "published_at": "1998-12-01T00:00:00.000Z"},
    {"key": "fan-cut", "name": "Fan Cut", "site": "YouTube", "type": "Trailer", "official": false, "published_at": "2001-05-01T00:00:00.000Z"},
    {"key": "vimeo-only", "name": "Vimeo Trailer", "site": "Vimeo", "type": "Trailer", "official": true, "published_at": "2001-06-01T00:00:00.000Z"},
    {"key": "aBcDeFgHiJk", "name": "Lobby Scene", "site": "YouTube", "type": "Clip", "official": true, "published_at": "2000-01-01T00:00:00.000Z"},
    {"key": "vKQi3bBA1y8", "name": "Official Trailer", "site": "YouTube", "type": "Trailer", "official": true, "published_at": "1999-02-01T00:00:00.000Z"}
  ]},
  "images": {
    "backdrops": [
      {"file_path": "/low.jpg", "width": 1920, "height": 1080, "vote_average": 5.1, "vote_count": 3},
      {"file_path": "/best.jpg", "width": 3840, "height": 2160, "vote_average": 5.4, "vote_count": 9},
      {"file_path": "", "width": 100, "height": 100, "vote_average": 9.9, "vote_count": 99},
      {"file_path": "/tie-b.jpg", "width": 1280, "height": 720, "vote_average": 5.1, "vote_count": 3}
    ],
    "posters": [
      {"file_path": "/p-low.jpg", "width": 1000, "height": 1500, "vote_average": 5.0, "vote_count": 4},
      {"file_path": "/p-top.jpg", "width": 2000, "height": 3000, "vote_average": 5.3, "vote_count": 12}
    ]
  },
  "watch/providers": {"results": {
    "US": {
      "link": "https://watch.example/movie/603?locale=US",
      "flatrate": [
        {"provider_id": 8, "provider_name": "Netflix", "logo_path": "/nf.jpg", "display_priority": 3},
        {"provider_id": 384, "provider_name": "HBO Max", "logo_path": "", "display_priority": 1}
      ],
      "ads": [
        {"provider_id": 613, "provider_name": "Freevee", "logo_path": "/fv.jpg", "display_priority": 9}
      ],
      "rent": [
        {"provider_id": 2, "provider_name": "Apple TV", "logo_path": "/atv.jpg", "display_priority": 4}
      ],
      "buy": [
        {"provider_id": 2, "provider_name": "Apple TV", "logo_path": "/atv.jpg", "display_priority": 4},
        {"provider_id": 10, "provider_name": "Amazon Video", "logo_path": "/av.jpg", "display_priority": 6}
      ]
    },
    "DE": {
      "link": "https://watch.example/movie/603?locale=DE",
      "flatrate": [
        {"provider_id": 30, "provider_name": "WOW", "logo_path": "/wow.jpg", "display_priority": 2}
      ]
    }
  }},
  "credits": {
    "cast": [
      {"name": "Carrie-Anne Moss", "character": "Trinity", "profile_path": "/t.jpg", "order": 1},
      {"id": 6384, "name": "Keanu Reeves", "character": "Neo", "profile_path": "/n.jpg", "order": 0}
    ],
    "crew": [
      {"name": "Joel Silver", "job": "Producer"},
      {"name": "Lana Wachowski", "job": "Director"}
    ]
  },
  "recommendations": {"page": 1, "total_pages": 1, "total_results": 4, "results": [
    {"id": 604, "title": "The Matrix Reloaded", "release_date": "2003-05-15", "poster_path": "/r.jpg", "vote_average": 7.0},
    {"id": 0, "title": "Ghost Entry", "release_date": "2001-01-01", "poster_path": "/g.jpg", "vote_average": 6.0},
    {"id": 605, "title": "", "release_date": "2003-11-05", "poster_path": "/rv.jpg", "vote_average": 6.7},
    {"id": 606, "title": "Untitled Sequel", "release_date": "", "poster_path": "", "vote_average": 0}
  ]}
}`

// ShowWithSeasonBody minus season 2 — the "before TMDB announced the new
// season" state, so a test can track the show, swap the stub to the two-season
// body and exercise how season 2 arrives.
const ShowWithOneSeasonBody = `{
  "id": 1396, "name": "Breaking Bad",
  "first_air_date": "2008-01-20",
  "seasons": [
    {"season_number": 1, "name": "Season 1", "episode_count": 7, "air_date": "2008-01-20"}
  ],
  "season/1": {
    "_id": "52542272760ee31328000094", "season_number": 1, "name": "Season 1",
    "air_date": "2008-01-20", "vote_average": 8.1,
    "episodes": [
      {"episode_number": 1, "name": "Pilot", "air_date": "2008-01-20",
       "runtime": 58, "still_path": "/p1.jpg", "vote_average": 8.2, "vote_count": 1500}
    ]
  }
}`
