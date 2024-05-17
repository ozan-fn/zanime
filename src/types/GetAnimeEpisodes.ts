export interface GetAnimeEpisodes {
    pagination: Pagination
    data: Daum[]
  }
  
  export interface Pagination {
    last_visible_page: number
    has_next_page: boolean
  }
  
  export interface Daum {
    mal_id: number
    url: string
    title: string
    title_japanese: string
    title_romanji: string
    aired: string
    score: number
    filler: boolean
    recap: boolean
    forum_url: string
  }
  