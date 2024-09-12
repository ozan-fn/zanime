import { createContext, Dispatch, SetStateAction, useContext, useEffect, useRef, useState } from "react";
import bg from "../assets/pexels-elijahsad-3473569.jpg";
import { SearchIcon, WandIcon } from "lucide-react";
import { AnimatePresence, motion } from "framer-motion";
import { Link, useLocation, Outlet, useNavigate } from "react-router-dom";

const ProviderContext = createContext<{ provider: string; setProvider: Dispatch<SetStateAction<string>> }>({ provider: "", setProvider: () => {} });

export const useProvider = () => {
    return useContext(ProviderContext);
};

export default function MainLayout() {
    const pathname = useLocation().pathname;
    const [search, setSearch] = useState({ enable: false, value: "" });
    const searchInputRef = useRef<HTMLInputElement>(null);
    const navigate = useNavigate();
    const [provider, setProvider] = useState("");
    const [modal, setModal] = useState(false);

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

    useEffect(() => {
        const ppp = localStorage.getItem("provider");
        if (!ppp) return setModal(true);
        setProvider(ppp);
    }, []);

    useEffect(() => {
        if (provider) {
            localStorage.setItem("provider", provider);
        }
        // const ttt = setTimeout(() => setModal(false), 3000);

        // return () => clearTimeout(ttt);
    }, [provider]);

    return (
        <ProviderContext.Provider value={{ provider, setProvider }}>
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
                                <div className="flex flex-row items-center gap-2">
                                    <button onClick={() => setModal((p) => !p)} className="h-10 w-10 rounded-lg hover:bg-white/10">
                                        <WandIcon className="mx-auto text-teal-500" />
                                    </button>
                                    <button onClick={handleSearchClick} className="aspect-square h-10 hover:rounded-lg hover:bg-white/10">
                                        <motion.div layoutId="_1" transition={{ type: "spring" }}>
                                            <SearchIcon className="mx-auto" />
                                        </motion.div>
                                    </button>
                                </div>
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

                {modal && (
                    <div className="absolute left-0 top-0 flex h-screen w-screen items-center justify-center bg-zinc-950/70">
                        <div className="flex w-full max-w-sm flex-col rounded-lg border border-white/10 bg-zinc-950/70 p-8 md:max-w-md">
                            {/* <div className="flex flex-row items-center gap-2 text-zinc-300"> */}
                            {/* <WandIcon className="text-teal-500" /> */}
                            {/* <div className="h-8"></div>
                        <p className="ml-auto text-xl font-medium">Select provider</p>
                    </div> */}
                            <div className="container mx-auto mt-4 flex flex-row items-center rounded-lg border border-white/10">
                                <div className="mr-4 h-10 w-1 rounded-lg bg-teal-500" />
                                <p className="text-lg font-medium text-white/70">
                                    SELECT <span className="text-teal-500">PROVIDER</span>
                                </p>
                            </div>

                            <div className="mt-4 grid grid-cols-2 gap-4 text-teal-500">
                                {["otakudesu", "kuronime"].map((v) => {
                                    const isSelected = v == provider;
                                    return (
                                        <>
                                            {isSelected ? (
                                                <>
                                                    <button onClick={() => setProvider(v)} className="relative flex h-14 items-center justify-center rounded-lg border border-teal-500">
                                                        {isSelected && <motion.div layoutId="_3" className="absolute inset-0 rounded-lg bg-teal-500"></motion.div>}
                                                        <button className="mix-blend-difference">{v.toUpperCase()}</button>
                                                    </button>
                                                </>
                                            ) : (
                                                <>
                                                    <button onClick={() => setProvider(v)} className="relative flex h-14 items-center justify-center rounded-lg border border-teal-500">
                                                        {isSelected && <motion.div layoutId="_3" className="absolute inset-0 rounded-lg bg-teal-500"></motion.div>}
                                                        <button className="mix-blend-difference">{v.toUpperCase()}</button>
                                                    </button>
                                                </>
                                            )}
                                        </>
                                    );
                                })}
                            </div>

                            <div className="ml-auto mt-4">
                                <button onClick={() => setModal((p) => !p)} className="h-10 rounded-lg bg-teal-500 px-4">
                                    CLOSE
                                </button>
                            </div>
                        </div>
                    </div>
                )}
            </div>
        </ProviderContext.Provider>
    );
}
