import app from "./app";

const port = process.env.PORT || 3000;

async function main() {
	app.listen(port, () => console.log(`Server is running on http://localhost:${port}`));
}
main();
