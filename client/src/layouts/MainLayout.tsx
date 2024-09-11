import { useRef, useState } from "react";
import bg from "../assets/pexels-elijahsad-3473569.jpg";
import { SearchIcon } from "lucide-react";
import { AnimatePresence, motion } from "framer-motion";
import { Link, useLocation, Outlet, useNavigate } from "react-router-dom";

export default function MainLayout() {
    const pathname = useLocation().pathname;
    const [search, setSearch] = useState({ enable: false, value: "" });
    const searchInputRef = useRef<HTMLInputElement>(null);
    const navigate = useNavigate();

    const handleSearchClick = () => {
        setSearch((prevState) => ({ ...prevState, enable: !prevState.enable }));
        if (!search.enable) {
            setTimeout(() => {
                searchInputRef.current?.focus();
            }, 0);
        }
    };

    const handleBlur = () => {
        setSearch((prevState) => ({ ...prevState, enable: false }));
    };

    const handleKeyPress = (event: { key: string }) => {
        if (event.key === "Enter") {
            if (!search.value) return;
            navigate("/search/" + search.value);
        }
    };

    return (
        <div style={{ backgroundImage: `url(${bg})` }}>
            <AnimatePresence mode="sync">
                <div className="flex h-screen flex-col overflow-auto px-4 text-zinc-300 backdrop-blur-md">
                    {/* HEADER */}
                    <div className="container mx-auto flex h-16 flex-shrink-0 flex-row items-center justify-between rounded-lg">
                        <Link to={"/"} className="text-2xl font-bold text-teal-500">
                            ZA
                            <span className="text-zinc-300">NIME</span>
                        </Link>
                        <motion.div layout transition={{ type: "spring" }} className="hidden flex-row items-center justify-center gap-8 md:flex">
                            <Link to={"/"} className="relative font-semibold">
                                <p>HOME</p>
                                {pathname == "/" && <motion.div transition={{ type: "spring" }} layoutId="_0" className="absolute -bottom-1 -left-0.5 h-1 w-9 rounded-lg bg-teal-500" />}
                            </Link>
                            <Link to={"/ongoing"} className="relative font-semibold">
                                <p>ON-GOING</p>
                                {pathname == "/ongoing" && <motion.div transition={{ type: "spring" }} layoutId="_0" className="absolute -bottom-1 -left-0.5 h-1 w-9 rounded-lg bg-teal-500" />}
                            </Link>
                            <Link to={"/genre"} className="relative font-semibold">
                                <p>GENRE</p>
                                {pathname == "/genre" && <motion.div transition={{ type: "spring" }} layoutId="_0" className="absolute -bottom-1 -left-0.5 h-1 w-9 rounded-lg bg-teal-500" />}
                            </Link>
                        </motion.div>
                        {!search.enable ? (
                            <button onClick={handleSearchClick} className="aspect-square h-10 hover:rounded-lg hover:bg-white/10">
                                <motion.div layoutId="_1" transition={{ type: "spring" }}>
                                    <SearchIcon className="mx-auto" />
                                </motion.div>
                            </button>
                        ) : (
                            <div className="flex h-10 flex-row items-center gap-2 rounded-lg bg-white/10 px-3">
                                <motion.div layoutId="_1" transition={{ type: "spring" }}>
                                    <SearchIcon className="mx-auto" />
                                </motion.div>
                                <motion.div animate={{ opacity: [0, 1] }} transition={{ delay: 0.3 }}>
                                    <input onKeyPress={handleKeyPress} onBlur={handleBlur} ref={searchInputRef} type="text" value={search.value} onChange={(e) => setSearch({ ...search, value: e.target.value })} className="bg-transparent outline-none" />
                                </motion.div>
                            </div>
                        )}
                    </div>

                    <div className="flex flex-grow flex-col">
                        <Outlet />
                    </div>

                    {/* FOOTER */}
                    <div className="container mx-auto flex h-16 flex-shrink-0 flex-row items-center">
                        <div className="relative">
                            <motion.p layoutId="fp" className="ml-auto whitespace-nowrap">
                                Made with 💖 by Akhmad Fauzan.
                            </motion.p>
                            <motion.div animate={{ x: ["0%", "140%", "0%"] }} transition={{ repeat: Infinity, duration: 2, delay: 1 }} className="absolute -bottom-1 -left-0.5 h-1 w-9 rounded-lg bg-teal-500" />
                        </div>
                    </div>
                </div>
            </AnimatePresence>
        </div>
    );
}
