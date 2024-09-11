import axios from "axios";
import * as cheerio from "cheerio";
import CryptoJS from "crypto-js";

const api = axios.create({ baseURL: "https://kuronime.pro" });

async function main() {
	const html = (await api.get(`/anime/mahouka-koukou-no-rettousei-season-3`)).data;
	const $ = cheerio.load(html);

	const data: {
		title: string;
		url: string;
	}[] = [];

	$(".bixbox.bxcl > ul > li").each((i, el) => {
		data[i] = {
			title: $(el).find(".lchx > a").text().trim(),
			url: $(el).find(".dt > a").attr("href")?.replace("https://kuronime.pro/", "") ?? "",
		};
	});

	console.log(data);
}
main();
