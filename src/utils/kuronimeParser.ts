import axios from "axios";
import * as cheerio from "cheerio";
import { AnimeOngoing, AnimeSearch, CompletedData, Embed, OngoingData } from "../types/animes";
import { AnimeCompleted, AnimeDetail } from "../../client/src/types/animes";
import CryptoJS from "crypto-js";

const api = axios.create({ baseURL: "https://kuronime.pro" });

async function search(query: string) {
	const html = (await api.get(`/?s=${query}`)).data;
	const $ = cheerio.load(html);

	const data: AnimeSearch[] = [];

	$(".listupd > .bs").each((i, el) => {
		data[i] = {
			title: $(el).find('h4[itemprop="headline"]').text().trim(),
			url: $(el).find('a[itemprop="url"]').attr("href")?.replace("https://kuronime.pro/anime/", "") ?? "",
			image: $(el).find("div > a > div > img").attr("data-src") ?? "",
			type: $(el).find("span.type").text().trim(),
			genres: [],
			rating: 0,
			status: "",
		};
	});

	return data;
}

async function ongoing(page: number = 1) {
	const html = (await api.get(`/ongoing-anime${(page ?? 0) > 1 ? `/page/${page}` : ``}`)).data;
	const $ = cheerio.load(html);

	const data: AnimeOngoing[] = [];

	$(".listupd > .bs").each((i, el) => {
		data[i] = {
			title: $(el).find('h4[itemprop="headline"]').text().trim(),
			url: $(el).find('a[itemprop="url"]').attr("href")?.replace("https://kuronime.pro/anime/", "") ?? "",
			type: $(el).find("span.type").text().trim(),
			image: $(el).find("div > a > div > img").attr("data-src") ?? "",
			date: "",
			day: "",
			episode: 0,
		};
	});

	const data2: OngoingData = { data, pagination: { currentPage: 0, totalPages: 0 } };

	return data2;
}

async function completed(page: number = 2) {
	const html = (await api.get(`/anime${page > 0 ? `/page/${page}` : ``}?title&status=completed&type=TV&order=latest`)).data;
	const $ = cheerio.load(html);

	const data: AnimeCompleted[] = [];

	$(".listupd > .bs").each((i, el) => {
		data[i] = {
			date: "",
			episodes: 0,
			image: $(el).find("img[itemprop='image']").attr("data-src") ?? "",
			rating: parseFloat($(el).find(".rating i").text()),
			title: $(el).find(".tt h4[itemprop='headline']").text(),
			url: $(el).find("a[itemprop='url']").attr("href")?.replace("https://kuronime.pro/anime", "").replace(/\//g, "") ?? "",
		};
	});

	const data2: CompletedData = {
		data,
		pagination: {
			currentPage: Number($('span[aria-current="page"]').text()),
			totalPages: +$("a.next.page-numbers").prev().text(),
		},
	};

	return data2;
}

async function episode(title: string) {
	const html = (await api.get(`/anime/${title}`)).data;
	const $ = cheerio.load(html);

	const data: AnimeDetail = {
		title: $(".entry-content li:contains('Judul')").text().split(": ")[1].split(",")[0],
		japaneseTitle: $(".entry-content li:contains('Judul')").text().split(": ")[1].split(",")[1].trim(),
		score: parseFloat($(".entry-content meta[itemprop='ratingValue']").attr("content") ?? ""),
		producer: "", // Tidak ada informasi produser dalam data
		type: $(".entry-content li:contains('Tipe')").text().split(": ")[1],
		status: $(".entry-content li:contains('Status')").text().split(": ")[1],
		totalEpisodes: $(".entry-content ul li:contains('Episode')").length, // Menghitung jumlah episode
		duration: $(".entry-content li:contains('Durasi')").text().split(": ")[1],
		releaseDate: $(".entry-content li:contains('Tayang')").text().split(": ")[1].split(" to")[0].trim(),
		studio: $(".entry-content li:contains('Studio') a").text(),
		genres: $(".entry-content li:contains('Genre') a")
			.map((i, el) => $(el).text())
			.get(),
		image: $(".entry-content .l img").attr("data-src") ?? "",
		synopsis: $(".entry-content .const p")
			.map((i, el) => $(el).text())
			.get()
			.join(" "),
		episodeList: $(".entry-content .bxcl ul li")
			.map((i, el) => ({
				title: $(el).find(".lchx a").text(),
				url: $(el).find(".lchx a").attr("href")?.replace("https://kuronime.pro", "").replace(/\//g, "") ?? "",
				date: "", // Tanggal tidak tersedia dalam data
			}))
			.get(),
	};

	return data;
}

async function embed(title: string = "nonton-rinkai-episode-2") {
	const html = (await api.get(`/${title}`)).data;
	const $ = cheerio.load(html);

	const t = $("#content > div > script:nth-child(2)");
	const token = t.toString().match('var _0xa100d42aa = "(.*)";')?.[1];

	if (!token) throw "token ngga ada walah";

	const mirror = await api("https://animeku.org/v3.1.php", {
		method: "post",
		headers: {
			"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
		},
		data: `id=${token}`,
	});

	const CryptoJSAesJson = {
		stringify: function (cipherParams: { ciphertext: { toString: (arg0: any) => any }; iv: { toString: () => any }; salt: { toString: () => any } }) {
			let jsonObj: { ct: string; iv?: string; s?: string } = {
				ct: cipherParams.ciphertext.toString(CryptoJS.enc.Base64),
			};
			if (cipherParams.iv) {
				jsonObj.iv = cipherParams.iv.toString();
			}
			if (cipherParams.salt) {
				jsonObj.s = cipherParams.salt.toString();
			}
			return JSON.stringify(jsonObj);
		},
		parse: function (jsonStr: string) {
			let jsonObj = JSON.parse(jsonStr);
			let cipherParams = CryptoJS.lib.CipherParams.create({
				ciphertext: CryptoJS.enc.Base64.parse(jsonObj.ct),
			});
			if (jsonObj.iv) {
				cipherParams.iv = CryptoJS.enc.Hex.parse(jsonObj.iv);
			}
			if (jsonObj.s) {
				cipherParams.salt = CryptoJS.enc.Hex.parse(jsonObj.s);
			}
			return cipherParams;
		},
	};

	function myDecriptor(encryptedText: string) {
		const options = {
			format: CryptoJSAesJson,
		};

		let decryptedData = JSON.parse(CryptoJS.AES.decrypt(encryptedText, "3&!Z0M,VIZ;dZW==", options).toString(CryptoJS.enc.Utf8));
		return decryptedData;
	}

	const data: Embed = myDecriptor(atob(mirror.data.mirror)).embed;

	return data;
}

const kuronimeParser = { search, ongoing, episode, completed, embed };

export default kuronimeParser;
