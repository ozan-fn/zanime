import axios from "axios";
import { Request, Response } from "express";
import kuronimeParser from "../utils/kuronimeParser";

export async function kuronimeSearch(req: Request, res: Response) {
	try {
		const query = req.query?.query as string;
		if (!query) throw "";
		const json = await kuronimeParser.search(query);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}

export async function kuronimeOngoing(req: Request, res: Response) {
	try {
		const page = req.query?.page as string;
		// if (!page) throw "";
		const json = await kuronimeParser.ongoing(+page);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}

export async function kuronimeCompleted(req: Request, res: Response) {
	try {
		const page = req.query?.page as string;
		// if (!page) throw "";
		const json = await kuronimeParser.completed(+page);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}

export async function kuronimeEpisode(req: Request, res: Response) {
	try {
		const title = req.params?.title;
		// if (!page) throw "";
		const json = await kuronimeParser.episode(title);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}

export async function kuronimeEmbed(req: Request, res: Response) {
	try {
		const episode = req.params?.episode;
		const json = await kuronimeParser.embed(episode);
		res.json(json);
	} catch (error) {
		if (error instanceof Error) {
			res.status(500).json({ status: 500, error: error.message });
			console.error(error.message);
		}
	}
}
