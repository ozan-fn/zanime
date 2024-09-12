export interface AnimeSearch {
    title: string;
    image: string;
    url: string;
    genres: string[];
    status: string;
    rating: number;
    type: string;
}

export interface AnimeOngoing {
    title: string;
    episode: number;
    day: string;
    date: string;
    image: string;
    url: string;
    type: string;
}

export interface Pagination {
    currentPage: number;
    totalPages: number;
}

export interface OngoingData {
    data: AnimeOngoing[];
    pagination: Pagination;
}

export interface AnimeCompleted {
    title: string;
    episodes: number;
    rating: number;
    date: string;
    image: string;
    url: string;
}

export interface CompletedData {
    data: AnimeCompleted[];
    pagination: Pagination;
}

export interface AnimeDetail {
    title: string;
    japaneseTitle: string;
    score: number;
    producer: string;
    type: string;
    status: string;
    totalEpisodes: number;
    duration: string;
    releaseDate: string;
    studio: string;
    genres: string[];
    image: string;
    synopsis: string;
    episodeList: { title: string; url: string; date: string }[];
}

export interface Embed {
    [resolution: string]: {
        [server: string]: string;
    };
}
