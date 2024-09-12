import express, { Request, Response } from "express";
import kuronimeRoute from "./routes/kuronimeRoute";
import cors from "cors";
import path from "path";
import otakudesuRouter from "./routes/otakudesuRoute";

const app = express();
const port = process.env.PORT || 3000;
app.use(cors());
app.use(express.static(path.join(__dirname, "../client/dist")));

app.get("/", (req: Request, res: Response) => res.send("Hello, TypeScript with Express!"));
app.use("/api/kuronime", kuronimeRoute);
app.use("/api/otakudesu", otakudesuRouter);

app.get("*", (req, res) => {
	res.sendFile(path.join(__dirname, "../client/dist/index.html"));
});

export default app;
