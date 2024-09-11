import { BrowserRouter, Route, Routes } from "react-router-dom";
import Home from "./pages/Home";
import Genre from "./pages/Genre";
import Ongoing from "./pages/Ongoing";
import MainLayout from "./layouts/MainLayout";
import Anime from "./pages/Anime";

export default function App() {
    return (
        <BrowserRouter>
            <Routes>
                <Route element={<MainLayout />}>
                    <Route path="/" element={<Home />} />
                    <Route path="/genre" element={<Genre />} />
                    <Route path="/ongoing" element={<Ongoing />} />
                    <Route path="/anime/:title" element={<Anime />} />
                </Route>
            </Routes>
        </BrowserRouter>
    );
}
