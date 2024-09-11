import axios from "axios";
import * as cheerio from "cheerio";
import { AnimeSearch } from "../types/animes";
// import fs from "fs";
// import path from "path";
import UserAgent from "user-agents";

const baseURL = "https://otakudesu.cloud";
const api = axios.create({ baseURL });
// const animes: { data: { title: string; animeSeason?: { year?: number }; type?: string; episodes?: number; picture?: string; synonyms?: string[] }[] } = JSON.parse(fs.readFileSync(path.join(__dirname, "../assets/anime-offline-database-minified_2.json"), "utf-8"));

async function otakudesuSearch(query = "made in abyss") {
	const html = (await api.get("/", { params: { s: query, post_type: "anime" } })).data;
	const $ = cheerio.load(html);

	const data: AnimeSearch[] = [];

	$(".chivsrc > li").each((i, el) => {
		const title = $(el).find("h2 > a").text().trim();
		const url = $(el).find("h2 > a").attr("href")?.replace("https://otakudesu.cloud/anime", "").replace(/\//g, "") ?? "";
		const image = $(el).find("img").attr("src") ?? "";
		const genres = $(el).find('.set:contains("Genres") a');
		const status = $(el).find('.set:contains("Status")').text().replace("Status :", "").trim();
		const rating = parseFloat($(el).find('.set:contains("Rating")').text().replace("Rating :", "").trim());

		data.push({ title, url, image, genres: genres.map((i, genreEl) => $(genreEl).text()).get(), status, rating });
	});

	console.log(data);
}
otakudesuSearch();
