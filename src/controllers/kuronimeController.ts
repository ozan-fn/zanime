import axios from "axios";
import * as cheerio from "cheerio";
import * as CryptoJS from "crypto-js";
import { Request, Response } from "express";

const api = axios.create({ baseURL: "https://kuronime.pro" });

export async function kuronimeEmbed(req: Request, res: Response) {
	try {
		const title = req.params?.title;
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

		res.json({
			status: 200,
			data: myDecriptor(atob(mirror.data.mirror)),
		});
	} catch (error) {
		res.sendStatus(500);
		console.error(error);
	}
}

export async function kuronimeSearch(req: Request, res: Response) {
	try {
		const page = req.query?.page;
		const query = req.query?.query;
		const html = (await api.get(`${page ? `/page/${page}` : ``}?s=${query}`)).data;
		const $ = cheerio.load(html);

		const data: {
			title: string;
			url?: string;
			type: string;
			imageUrl?: string;
		}[] = [];

		$(".listupd > .bs").each((i, el) => {
			data[i] = {
				title: $(el).find('h4[itemprop="headline"]').text().trim(),
				url: $(el).find('a[itemprop="url"]').attr("href")?.replace("https://kuronime.pro/anime/", ""),
				type: $(el).find("span.type").text().trim(),
				imageUrl: $(el).find("div > a > div > img").attr("data-src"),
			};
		});

		res.json({
			data,
			total: $(`.pagination > .page-numbers:nth-child(${(Number(page) ?? 0) > 1 ? `7` : `5`})`)
				.text()
				.trim(),
		});
	} catch (error) {
		res.sendStatus(500);
		console.error(error);
	}
}

export async function kuronimeOngoing(req: Request, res: Response) {
	const page = req.query?.page;
	const html = (await api.get(`/ongoing-anime${page ? `/page/${page}` : ``}`)).data;
	const $ = cheerio.load(html);

	const data: {
		title: string;
		url?: string;
		type: string;
		imageUrl?: string;
	}[] = [];

	$(".listupd > .bs").each((i, el) => {
		data[i] = {
			title: $(el).find('h4[itemprop="headline"]').text().trim(),
			url: $(el).find('a[itemprop="url"]').attr("href")?.replace("https://kuronime.pro/anime/", ""),
			type: $(el).find("span.type").text().trim(),
			imageUrl: $(el).find("div > a > div > img").attr("data-src"),
		};
	});

	res.json({
		data,
		total: $(`.pagination > .page-numbers:nth-child(${(Number(page) || 0) > 1 ? `7` : `5`})`)
			.text()
			.trim(),
	});
}

export async function kuronimeCompleted(req: Request, res: Response) {
	const page = req.query?.page;
	const html = (await api.get(`/anime${page ? `/page/${page}` : ``}?title=&status=completed&type=&order=update`)).data;
	const $ = cheerio.load(html);

	const data: {
		title: string;
		url?: string;
		type: string;
		imageUrl?: string;
	}[] = [];

	$(".listupd > .bs").each((i, el) => {
		data[i] = {
			title: $(el).find('h4[itemprop="headline"]').text().trim(),
			url: $(el).find('a[itemprop="url"]').attr("href")?.replace("https://kuronime.pro/anime/", ""),
			type: $(el).find("span.type").text().trim(),
			imageUrl: $(el).find("div > a > div > img").attr("data-src"),
		};
	});

	res.json({
		data,
		total: $(`.pagination > .page-numbers:nth-child(${(Number(page) || 0) > 1 ? `7` : `5`})`)
			.text()
			.trim(),
	});
}

export async function kuronimeEpisode(req: Request, res: Response) {
	const title = req.params?.title;
	const html = (await api.get(`/anime/${title}`)).data;
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

	res.json({
		data,
	});
}
