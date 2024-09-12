import axios from "axios";
import * as cheerio from "cheerio";
import { AnimeCompleted, AnimeDetail, AnimeOngoing, AnimeSearch, CompletedData, Embed, OngoingData, Pagination } from "../types/animes";
// import fs from "fs";
// import path from "path";
import UserAgent from "user-agents";

const baseURL = "https://otakudesu.cloud";
const api = axios.create({ baseURL, proxy: { protocol: "http:", host: "172.67.176.55", port: 80 } });
// const animes: { data: { title: string; animeSeason?: { year?: number }; type?: string; episodes?: number; picture?: string; synonyms?: string[] }[] } = JSON.parse(fs.readFileSync(path.join(__dirname, "../assets/anime-offline-database-minified_2.json"), "utf-8"));

async function search(query: string) {
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

		data.push({ title, url, image, genres: genres.map((i, genreEl) => $(genreEl).text()).get(), status, rating, type: "" });
	});

	return data;
}

async function ongoing(page = 1) {
	const html = (await api.get("/ongoing-anime" + (page > 1 ? `/page/${page}` : ""))).data;
	const $ = cheerio.load(html);

	const data: AnimeOngoing[] = [];

	$(".venz ul li").each((i, el) => {
		const title = $(el).find("h2.jdlflm").text().trim();
		const episodeText = $(el).find(".epz").text().replace("Episode", "").trim();
		const episode = parseInt(episodeText, 10);
		const day = $(el).find(".epztipe").text().trim();
		const date = $(el).find(".newnime").text().trim();
		const image = $(el).find(".thumbz img").attr("src") ?? "";
		const url = $(el).find("a").attr("href")?.replace("https://otakudesu.cloud/anime", "").replace(/\//g, "") ?? "";

		data.push({ title, episode, day, date, image, url, type: "" });
	});

	const lastPageElement = $(".pagenavix .page-numbers").filter((i, el) => !isNaN(parseInt($(el).text())));
	const pagination: Pagination = {
		currentPage: +$(".pagenavix .page-numbers.current").text().trim(),
		totalPages: +lastPageElement.last().text(),
	};

	const data2: OngoingData = { data: data, pagination };

	return data2;
}

async function completed(page = 1) {
	const html = (await api.get("/complete-anime" + (page > 1 ? `/page/${page}` : ""))).data;
	const $ = cheerio.load(html);

	const data: AnimeCompleted[] = [];

	$(".venz ul li").each((i, el) => {
		const title = $(el).find("h2.jdlflm").text().trim();
		const episodeText = $(el).find(".epz").text().replace("Episode", "").trim();
		const episodes = parseInt(episodeText, 10);
		const ratingText = $(el).find(".epztipe").text().trim();
		const rating = parseFloat(ratingText);
		const date = $(el).find(".newnime").text().trim();
		const image = $(el).find(".thumbz img").attr("src") ?? "";
		const url = $(el).find("a").attr("href")?.replace("https://otakudesu.cloud/anime", "").replace(/\//g, "") ?? "";

		data.push({ title, episodes, rating, date, image, url });
	});

	const lastPageElement = $(".pagenavix .page-numbers").filter((i, el) => !isNaN(parseInt($(el).text())));
	const pagination: Pagination = {
		currentPage: +$(".pagenavix .page-numbers.current").text().trim(),
		totalPages: +lastPageElement.last().text(),
	};

	const data2: CompletedData = { data: data, pagination };

	return data2;
}

async function episode(title: string) {
	const html = (await api.get("/anime/" + title)).data;
	const $ = cheerio.load(html);

	const data: AnimeDetail = {
		title: $('b:contains("Judul")').parent().text().replace("Judul: ", "").trim(),
		japaneseTitle: $('b:contains("Japanese")').parent().text().replace("Japanese: ", "").trim(),
		score: parseFloat($('b:contains("Skor")').parent().text().replace("Skor: ", "").trim()),
		producer: $('b:contains("Produser")').parent().text().replace("Produser: ", "").trim(),
		type: $('b:contains("Tipe")').parent().text().replace("Tipe: ", "").trim(),
		status: $('b:contains("Status")').parent().text().replace("Status: ", "").trim(),
		totalEpisodes: parseInt($('b:contains("Total Episode")').parent().text().replace("Total Episode: ", "").trim()),
		duration: $('b:contains("Durasi")').parent().text().replace("Durasi: ", "").trim(),
		releaseDate: $('b:contains("Tanggal Rilis")').parent().text().replace("Tanggal Rilis: ", "").trim(),
		studio: $('b:contains("Studio")').parent().text().replace("Studio: ", "").trim(),
		genres: $('b:contains("Genre")')
			.parent()
			.find("a")
			.map((i, el) => $(el).text())
			.get(),
		image: $("div.fotoanime img").attr("src") ?? "",
		synopsis: $(".sinopc").text().trim(),
		episodeList: [],
	};

	const episodeListSection = $(".episodelist").eq(1); // Ambil elemen kedua dari semua elemen dengan class 'episodelist'

	const episodeList = episodeListSection
		.find("ul li")
		.map((i, el) => {
			const title = $(el).find("a").text().trim();
			const url = $(el).find("a").attr("href")?.replace("https://otakudesu.cloud/episode", "").replace(/\//g, "") ?? "";
			const date = $(el).find(".zeebr").text().trim();
			return { title, url, date };
		})
		.get();

	data.episodeList = episodeList;

	return data;
}

async function embed(episode: string = "tbrd-episode-1-sub-indo") {
	const html = (await api.get("/episode/" + episode)).data;
	const $ = cheerio.load(html);

	const data: { [key: string]: { server: string; token: string; nonce: string; action: string }[] } = {};
	const action1 = (html as string).match(/action:"(.*?)"/g)?.[1]?.match(/action:"(.*?)"/)?.[1];
	const action2 = (html as string).match(/action:"(.*?)"/g)?.[0]?.match(/action:"(.*?)"/)?.[1] ?? "";
	const nonce: string = (await api.post("https://otakudesu.cloud/wp-admin/admin-ajax.php", `action=${action1}`)).data.data;

	cheerio
		.load(html)("div.mirrorstream ul")
		.each((i, el) => {
			const servers: { server: string; token: string; nonce: string; action: string }[] = [];
			$(el)
				.find("li")
				.each((i, el) => {
					const server = $(el).find("a").text().trim();
					const token = $(el).find("a").attr("data-content") ?? "";
					servers.push({ server, token, nonce, action: action2 });
				});

			data[$(el).attr("class") ?? "unknown"] = servers;
		});

	const data2: Embed = {};

	async function fetchUrl(entry: { token: string; server: string; nonce: string; action: string }): Promise<{ server: string; url: string }> {
		const { id, i, q }: { id: number; i: number; q: string } = JSON.parse(atob(entry.token));
		const response = await api.post("https://otakudesu.cloud/wp-admin/admin-ajax.php", `id=${id}&i=${i}&q=${q}&nonce=${entry.nonce}&action=${entry.action}`);
		const $ = cheerio.load(atob(response.data.data));
		const url = $("iframe").attr("src") || "";
		return { server: entry.server, url };
	}

	for (let res of Object.keys(data)) {
		var results: { [server: string]: string } = {};
		await Promise.all(
			Object.keys(data).map(async (resolution) => {
				const resolutionResults = await Promise.all(data[resolution].map((entry) => fetchUrl(entry)));
				resolutionResults.forEach((v) => {
					results[v.server] = v.url;
				});
			})
		);
		data2[res] = results;
	}

	return data2;
}

const otakudesuParser = { search, ongoing, completed, episode, embed };

export default otakudesuParser;
