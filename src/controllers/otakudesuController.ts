import { Request, Response } from "express";
import otakudesuParser from "../utils/otakudesuParser";

export async function otakudesuSearch(req: Request, res: Response) {
	try {
		const query = req.query?.query as string;
		if (!query) throw "";
		const json = await otakudesuParser.search(query);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}

export async function otakudesuOngoing(req: Request, res: Response) {
	try {
		const page = Number(req.query?.page);
		const json = await otakudesuParser.ongoing(page > 1 ? page : 1);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}

export async function otakudesuCompleted(req: Request, res: Response) {
	try {
		const page = Number(req.query?.page);
		const json = await otakudesuParser.completed(page > 1 ? page : 1);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}

export async function otakudesuEpisode(req: Request, res: Response) {
	try {
		const title = req.params.title;
		const json = await otakudesuParser.episode(title);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}

export async function otakudesuEmbed(req: Request, res: Response) {
	try {
		const episode = req.params.episode;
		const json = await otakudesuParser.embed(episode);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}
